// Command mockupstream serves a local upstream for the act load test.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:9090", "loopback listen address")
	delay := flag.Duration("delay", 0, "fixed upstream delay, for example 5ms")
	flag.Parse()
	if *delay < 0 {
		log.Fatal("delay must be non-negative")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "ok\n")
	})
	mux.HandleFunc("POST /echo", echo(*delay))
	log.Printf("mock upstream on %s, delay %s", *addr, *delay)
	server := &http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	log.Fatal(server.ListenAndServe())
}

func echo(delay time.Duration) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var params json.RawMessage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&params); err != nil {
			http.Error(w, "invalid JSON params", http.StatusBadRequest)
			return
		}
		if r.Header.Get("Authorization") == "" {
			http.Error(w, "missing injected token", http.StatusUnauthorized)
			return
		}
		if delay > 0 {
			timer := time.NewTimer(delay)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-r.Context().Done():
				return
			}
		}
		w.Header().Set("Content-Type", "application/json")
		// Exclude response encoding and writing from this timestamp. Their cost stays in the overhead estimate.
		fmt.Fprintf(w, `{"ok":true,"upstream_ns":%d}`, time.Since(start).Nanoseconds())
	}
}
