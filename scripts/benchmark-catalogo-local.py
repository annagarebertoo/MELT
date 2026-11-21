#!/usr/bin/env python3
"""Bounded, local-only MELT catalogue benchmark. No production traffic."""
import argparse
import concurrent.futures
import json
import math
import statistics
import time
import urllib.error
import urllib.parse
import urllib.request


def percentile(values, fraction):
    ordered = sorted(values)
    if not ordered:
        return None
    return round(ordered[math.ceil((len(ordered) - 1) * fraction)], 2)


def request(url, timeout):
    start = time.perf_counter()
    try:
        with urllib.request.urlopen(url, timeout=timeout) as response:
            response.read()
            status = response.status
    except urllib.error.HTTPError as exc:
        status = exc.code
    except (urllib.error.URLError, TimeoutError, OSError):
        status = 0
    return status, (time.perf_counter() - start) * 1000


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--url", default="http://localhost:8080/api/v1/tratamientos?page=1&limit=10")
    parser.add_argument("--requests", type=int, default=40)
    parser.add_argument("--concurrency", type=int, default=2)
    parser.add_argument("--timeout", type=float, default=5.0)
    parser.add_argument("--output", default="")
    args = parser.parse_args()
    parsed = urllib.parse.urlsplit(args.url)
    if parsed.scheme != "http" or parsed.hostname not in {"localhost", "127.0.0.1", "::1"}:
        parser.error("Only local HTTP endpoints are permitted; do not load-test Render or other remote systems.")
    if not (1 <= args.requests <= 200 and 1 <= args.concurrency <= 10 and 0.1 <= args.timeout <= 15):
        parser.error("Limits: 1-200 requests, 1-10 workers, timeout 0.1-15 seconds.")
    start = time.perf_counter()
    with concurrent.futures.ThreadPoolExecutor(max_workers=args.concurrency) as pool:
        results = list(pool.map(lambda _: request(args.url, args.timeout), range(args.requests)))
    duration = time.perf_counter() - start
    statuses = {}
    for status, _ in results:
        statuses[str(status)] = statuses.get(str(status), 0) + 1
    latencies = [elapsed for _, elapsed in results]
    summary = {
        "url": args.url, "requests": args.requests, "concurrency": args.concurrency,
        "elapsed_seconds": round(duration, 3), "requests_per_second": round(args.requests / duration, 2),
        "status_counts": statuses, "errors": sum(status < 200 or status >= 400 for status, _ in results),
        "latency_ms": {"p50": percentile(latencies, .50), "p95": percentile(latencies, .95),
                       "p99": percentile(latencies, .99), "mean": round(statistics.mean(latencies), 2)}
    }
    result = json.dumps(summary, indent=2, ensure_ascii=False)
    print(result)
    if args.output:
        from pathlib import Path
        Path(args.output).write_text(result + "\n", encoding="utf-8")


if __name__ == "__main__":
    main()
