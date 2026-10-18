// Command loadgen measures scheduled latency without coordinated omission.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"net/http/httptrace"
	"os"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
)

type sample struct {
	Total, Upstream, Dispatch, Auth, Receipt, Handler, ClientConn int64
	Reused                                                        bool
	Error                                                         string
}

func percentile(values []int64, p float64) float64 {
	slices.Sort(values)
	return float64(values[max(0, int(math.Ceil(float64(len(values))*p))-1)]) / 1e6
}

func summarizeLatencies(samples []sample) map[string]any {
	overhead, dispatch := make([]int64, 0, len(samples)), make([]int64, 0, len(samples))
	failures := 0
	for _, s := range samples {
		if s.Error != "" {
			failures++
			continue
		}
		overhead = append(overhead, s.Total-s.Upstream)
		dispatch = append(dispatch, s.Dispatch)
	}
	summary := map[string]any{
		"errors": failures, "successful": len(overhead), "valid": failures == 0 && len(overhead) > 0,
		"p50_ms": nil, "p90_ms": nil, "p99_ms": nil, "p999_ms": nil, "dispatch_p99_ms": nil,
	}
	if len(overhead) == 0 {
		return summary
	}
	summary["p50_ms"] = percentile(slices.Clone(overhead), .50)
	summary["p90_ms"] = percentile(slices.Clone(overhead), .90)
	summary["p999_ms"] = percentile(slices.Clone(overhead), .999)
	summary["dispatch_p99_ms"] = percentile(dispatch, .99)
	if failures == 0 {
		summary["p99_ms"] = percentile(overhead, .99)
	}
	return summary
}

func waitUntil(deadline time.Time) {
	for {
		left := time.Until(deadline)
		if left <= 0 {
			return
		}
		if left > 2*time.Millisecond {
			time.Sleep(left - time.Millisecond)
		} else {
			runtime.Gosched()
		}
	}
}

func main() {
	url := flag.String("url", "http://127.0.0.1:18080/v1/act", "gateway URL")
	credentials := flag.String("credentials", ".cache/proc/key.json", "credential JSON file")
	count := flag.Int("n", 20000, "measured requests")
	rate := flag.Int("rate", 1000, "fixed arrivals per second; 0 selects closed loop")
	concurrency := flag.Int("c", 16, "closed-loop concurrency")
	output := flag.String("output", ".cache/proc/samples.csv", "per-request CSV path")
	flag.Parse()
	if *count <= 0 || *rate < 0 || *concurrency <= 0 {
		log.Fatal("n and c must be positive; rate must be non-negative")
	}
	data, err := os.ReadFile(*credentials)
	if err != nil {
		log.Fatal(err)
	}
	var secret struct{ Key string }
	if err := json.Unmarshal(data, &secret); err != nil || secret.Key == "" {
		log.Fatal("invalid credential file")
	}
	tr := &http.Transport{MaxIdleConns: 128, MaxIdleConnsPerHost: 128, MaxConnsPerHost: 128}
	defer tr.CloseIdleConnections()
	client := &http.Client{Transport: tr, Timeout: 15 * time.Second}
	act := func(scheduled time.Time, id int) sample {
		const body = `{"service":"mock","action":"echo","on_behalf_of":"bench-user","params":{"worker":0}}`
		req, _ := http.NewRequest(http.MethodPost, *url, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+secret.Key)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Bench-ID", strconv.Itoa(id))
		result := sample{}
		var start time.Time
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) {
				result.Reused = info.Reused
				result.ClientConn = time.Since(start).Nanoseconds()
			},
		}))
		start = time.Now()
		if scheduled.IsZero() {
			scheduled = start
		}
		result.Dispatch = start.Sub(scheduled).Nanoseconds()
		resp, err := client.Do(req)
		if err != nil {
			result.Error = err.Error()
			return result
		}
		data, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		result.Total = time.Since(scheduled).Nanoseconds()
		if err != nil {
			result.Error = err.Error()
			return result
		}
		var envelope struct {
			Status int
			Body   struct {
				OK         bool  `json:"ok"`
				UpstreamNS int64 `json:"upstream_ns"`
			}
		}
		if err := json.Unmarshal(data, &envelope); err != nil || resp.StatusCode != 200 || envelope.Status != 200 || !envelope.Body.OK {
			result.Error = fmt.Sprintf("status %d: %s", resp.StatusCode, data)
			return result
		}
		result.Upstream = envelope.Body.UpstreamNS
		result.Auth, _ = strconv.ParseInt(resp.Header.Get("X-Bench-Auth-Ns"), 10, 64)
		result.Receipt, _ = strconv.ParseInt(resp.Header.Get("X-Bench-Receipt-Ns"), 10, 64)
		result.Handler, _ = strconv.ParseInt(resp.Header.Get("X-Bench-Handler-Ns"), 10, 64)
		if result.Upstream < 0 || result.Upstream > result.Total {
			result.Error = "invalid upstream duration"
		}
		return result
	}
	type snapshot struct {
		Head         uint64
		TotalAlloc   uint64 `json:"total_alloc"`
		NumGC        uint64 `json:"num_gc"`
		PauseTotalNS uint64 `json:"pause_total_ns"`
	}
	head := func() snapshot {
		base := strings.TrimSuffix(*url, "/v1/act")
		resp, err := client.Get(base + "/bench/head")
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		var body snapshot
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != 200 {
			log.Fatal("cannot read ledger head")
		}
		return body
	}
	for range 256 {
		if warmup := act(time.Time{}, -1); warmup.Error != "" {
			log.Fatal(warmup.Error)
		}
	}
	before := head()
	samples := make([]sample, *count)
	start := time.Now().Add(10 * time.Millisecond)
	var done sync.WaitGroup
	if *rate > 0 {
		for i := range samples {
			scheduled := start.Add(time.Duration(int64(i) * int64(time.Second) / int64(*rate)))
			waitUntil(scheduled)
			done.Add(1)
			go func() {
				defer done.Done()
				samples[i] = act(scheduled, i)
			}()
		}
	} else {
		waitUntil(start)
		for worker := range min(*concurrency, *count) {
			done.Add(1)
			go func() {
				defer done.Done()
				for i := worker; i < *count; i += *concurrency {
					samples[i] = act(time.Time{}, i)
				}
			}()
		}
	}
	done.Wait()
	elapsed := time.Since(start)
	after := head()
	file, err := os.Create(*output)
	if err != nil {
		log.Fatal(err)
	}
	writer := csv.NewWriter(file)
	writer.Write([]string{"total_ns", "upstream_ns", "dispatch_ns", "auth_ns", "receipt_ns", "handler_ns", "client_conn_ns", "reused", "error"})
	reused := 0
	for _, s := range samples {
		writer.Write([]string{
			strconv.FormatInt(s.Total, 10), strconv.FormatInt(s.Upstream, 10), strconv.FormatInt(s.Dispatch, 10),
			strconv.FormatInt(s.Auth, 10), strconv.FormatInt(s.Receipt, 10), strconv.FormatInt(s.Handler, 10),
			strconv.FormatInt(s.ClientConn, 10),
			strconv.FormatBool(s.Reused), s.Error,
		})
		if s.Reused {
			reused++
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		log.Fatal(err)
	}
	if err := file.Close(); err != nil {
		log.Fatal(err)
	}
	summary := summarizeLatencies(samples)
	for key, value := range map[string]any{
		"rate": *rate, "c": *concurrency, "n": *count, "receipts": after.Head - before.Head,
		"throughput":                      float64(summary["successful"].(int)) / elapsed.Seconds(),
		"reuse_pct":                       100 * float64(reused) / float64(*count),
		"gateway_alloc_bytes_per_request": float64(after.TotalAlloc-before.TotalAlloc) / float64(*count),
		"gateway_gc_cycles":               after.NumGC - before.NumGC,
		"gateway_gc_pause_total_ms":       float64(after.PauseTotalNS-before.PauseTotalNS) / 1e6,
	} {
		summary[key] = value
	}
	if after.Head-before.Head != uint64(*count) {
		summary["valid"], summary["p99_ms"] = false, nil
	}
	json.NewEncoder(os.Stdout).Encode(summary)
	if !summary["valid"].(bool) {
		os.Exit(1)
	}
}
