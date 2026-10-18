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
	Total, Upstream, Dispatch, Auth, Receipt, Handler int64
	Reused                                            bool
	Error                                             string
}

func percentile(values []int64, p float64) float64 {
	slices.Sort(values)
	return float64(values[max(0, int(math.Ceil(float64(len(values))*p))-1)]) / 1e6
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
		req = req.WithContext(httptrace.WithClientTrace(req.Context(), &httptrace.ClientTrace{
			GotConn: func(info httptrace.GotConnInfo) { result.Reused = info.Reused },
		}))
		start := time.Now()
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
	head := func() uint64 {
		base := strings.TrimSuffix(*url, "/v1/act")
		resp, err := client.Get(base + "/bench/head")
		if err != nil {
			log.Fatal(err)
		}
		defer resp.Body.Close()
		var body struct{ Head uint64 }
		if err := json.NewDecoder(resp.Body).Decode(&body); err != nil || resp.StatusCode != 200 {
			log.Fatal("cannot read ledger head")
		}
		return body.Head
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
	writer.Write([]string{"total_ns", "upstream_ns", "dispatch_ns", "auth_ns", "receipt_ns", "handler_ns", "reused", "error"})
	overhead, dispatch := make([]int64, *count), make([]int64, *count)
	errors, reused := 0, 0
	for i, s := range samples {
		writer.Write([]string{
			strconv.FormatInt(s.Total, 10), strconv.FormatInt(s.Upstream, 10), strconv.FormatInt(s.Dispatch, 10),
			strconv.FormatInt(s.Auth, 10), strconv.FormatInt(s.Receipt, 10), strconv.FormatInt(s.Handler, 10),
			strconv.FormatBool(s.Reused), s.Error,
		})
		overhead[i], dispatch[i] = s.Total-s.Upstream, s.Dispatch
		if s.Error != "" {
			errors++
		}
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
	summary := map[string]any{
		"rate": *rate, "c": *concurrency, "n": *count, "errors": errors, "receipts": after - before,
		"throughput": float64(*count) / elapsed.Seconds(), "reuse_pct": 100 * float64(reused) / float64(*count),
		"p50_ms": percentile(slices.Clone(overhead), .50), "p90_ms": percentile(slices.Clone(overhead), .90),
		"p99_ms": percentile(slices.Clone(overhead), .99), "p999_ms": percentile(overhead, .999),
		"dispatch_p99_ms": percentile(dispatch, .99),
	}
	json.NewEncoder(os.Stdout).Encode(summary)
	if errors != 0 || after-before != uint64(*count) {
		os.Exit(1)
	}
}
