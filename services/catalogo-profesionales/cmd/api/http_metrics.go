package main

import (
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

var httpDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5}

type httpMetricKey struct {
	method  string
	route   string
	status  string
	replica string
}

type httpMetricSample struct {
	count   uint64
	sum     float64
	buckets []uint64
}

type httpMetricRegistry struct {
	mu      sync.RWMutex
	samples map[httpMetricKey]*httpMetricSample
	replica string
}

var catalogHTTPMetrics = newHTTPMetricRegistry()

func newHTTPMetricRegistry() *httpMetricRegistry {
	replica, err := os.Hostname()
	if err != nil || strings.TrimSpace(replica) == "" {
		replica = "unknown"
	}
	return &httpMetricRegistry{samples: make(map[httpMetricKey]*httpMetricSample), replica: replica}
}

func normalizeHTTPMethod(method string) string {
	switch method {
	case "GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS":
		return method
	default:
		return "OTHER"
	}
}

func (r *httpMetricRegistry) record(method, route string, status int, duration time.Duration) {
	if route == "" {
		route = "unmatched"
	}
	key := httpMetricKey{
		method: normalizeHTTPMethod(method), route: route,
		status: strconv.Itoa(status), replica: r.replica,
	}
	seconds := duration.Seconds()
	if seconds < 0 {
		seconds = 0
	}
	r.mu.Lock()
	sample := r.samples[key]
	if sample == nil {
		sample = &httpMetricSample{buckets: make([]uint64, len(httpDurationBuckets))}
		r.samples[key] = sample
	}
	sample.count++
	sample.sum += seconds
	for index, bound := range httpDurationBuckets {
		if seconds <= bound {
			sample.buckets[index]++
		}
	}
	r.mu.Unlock()
}

func (r *httpMetricRegistry) writePrometheus(w io.Writer) {
	fmt.Fprintln(w, "# HELP catalog_replica_info Identity of this Catálogo replica.")
	fmt.Fprintln(w, "# TYPE catalog_replica_info gauge")
	fmt.Fprintf(w, "catalog_replica_info{replica=%q} 1\n", r.replica)
	fmt.Fprintln(w, "# HELP catalog_http_requests_total HTTP requests handled by Catálogo.")
	fmt.Fprintln(w, "# TYPE catalog_http_requests_total counter")
	fmt.Fprintln(w, "# HELP catalog_http_request_duration_seconds HTTP request duration in seconds.")
	fmt.Fprintln(w, "# TYPE catalog_http_request_duration_seconds histogram")

	r.mu.RLock()
	keys := make([]httpMetricKey, 0, len(r.samples))
	for key := range r.samples {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		a, b := keys[i], keys[j]
		return strings.Join([]string{a.replica, a.method, a.route, a.status}, "\x00") <
			strings.Join([]string{b.replica, b.method, b.route, b.status}, "\x00")
	})
	for _, key := range keys {
		sample := r.samples[key]
		labels := fmt.Sprintf("method=%q,route=%q,status=%q,replica=%q", key.method, key.route, key.status, key.replica)
		fmt.Fprintf(w, "catalog_http_requests_total{%s} %d\n", labels, sample.count)
		for index, bound := range httpDurationBuckets {
			fmt.Fprintf(w, "catalog_http_request_duration_seconds_bucket{%s,le=%q} %d\n", labels, strconv.FormatFloat(bound, 'g', -1, 64), sample.buckets[index])
		}
		fmt.Fprintf(w, "catalog_http_request_duration_seconds_bucket{%s,le=\"+Inf\"} %d\n", labels, sample.count)
		fmt.Fprintf(w, "catalog_http_request_duration_seconds_sum{%s} %g\n", labels, sample.sum)
		fmt.Fprintf(w, "catalog_http_request_duration_seconds_count{%s} %d\n", labels, sample.count)
	}
	r.mu.RUnlock()
}
