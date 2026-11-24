#!/usr/bin/env python3
"""Bounded load comparison for isolated MELT S1, with optional guarded failover."""

from __future__ import annotations

import argparse
from collections import Counter
from concurrent.futures import ThreadPoolExecutor
import datetime as dt
import importlib.util
import json
import math
from pathlib import Path
import re
import signal
import subprocess
import sys
import threading
import time
import uuid


ROOT = Path(__file__).resolve().parents[1]
SCRIPTS = ROOT / "scripts"
S1_SCRIPT = SCRIPTS / "verify-s1-failover.py"
BENCHMARK_SCRIPT = SCRIPTS / "benchmark-catalogo-local.py"
PROJECT = "melt-s1"
SERVICE = "catalogo-profesionales"
ENDPOINT = "http://127.0.0.1/api/v1/catalogo/tratamientos?page=1&limit=10"
HTTP_STATUS_RE = re.compile(r"HTTP/\d(?:\.\d)?\s+(\d{3})")

# The S1 verifier owns the Docker Desktop, Compose isolation, labels, health,
# network, and restoration guards. Reuse those checks without changing S1.
sys.dont_write_bytecode = True


def load_module(name: str, path: Path):
    spec = importlib.util.spec_from_file_location(name, path)
    if spec is None or spec.loader is None:
        raise RuntimeError(f"Cannot load helper module: {path}")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


s1 = load_module("melt_s1_failover_guards", S1_SCRIPT)
benchmark = load_module("melt_local_benchmark", BENCHMARK_SCRIPT)


def timestamp() -> str:
    return dt.datetime.now(s1.ART).isoformat(timespec="milliseconds")


def write_report(path: Path, report: dict) -> None:
    path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def request_once(gateway_id: str, phase: str, sequence: int, run_id: str, timeout: float) -> dict:
    request_id = f"s3-{run_id}-{phase}-{sequence:05d}"
    start = time.perf_counter()
    error = None
    status = None
    output = ""
    try:
        result = s1.docker(
            "exec", "-i", gateway_id, "wget", "-S", "-q", "-O", "/dev/null",
            "-T", str(max(1, math.ceil(timeout))),
            "--header", f"X-Request-ID: {request_id}", ENDPOINT,
            timeout=max(15, math.ceil(timeout) + 10), check=False,
        )
        output = result.stdout + "\n" + result.stderr
        matches = HTTP_STATUS_RE.findall(output)
        status = int(matches[-1]) if matches else None
        if status is None:
            error = {"kind": "transport", "message": output.strip()[-500:] or f"docker exec exit {result.returncode}"}
        elif not 200 <= status < 300:
            error = {"kind": "http", "message": f"Unexpected HTTP {status}"}
    except subprocess.TimeoutExpired as exc:
        error = {"kind": "timeout", "message": str(exc)[:500]}
    except (OSError, RuntimeError) as exc:
        error = {"kind": "client", "message": f"{type(exc).__name__}: {str(exc)[:500]}"}
    elapsed_ms = (time.perf_counter() - start) * 1000
    return {
        "phase": phase,
        "sequence": sequence,
        "request_id": request_id,
        "http_status": status,
        "client_wall_ms": round(elapsed_ms, 3),
        "measurement_note": "Includes Docker exec startup and the HTTP round trip from the local runner.",
        "error": error,
        "replica_that_handled": None,
        "gateway_log_found": False,
        "catalog_log_found": False,
    }


def run_phase(
    gateway_id: str,
    phase: str,
    run_id: str,
    concurrency: int,
    timeout: float,
    request_limit: int,
    duration_seconds: float | None,
) -> tuple[list[dict], float]:
    started = time.perf_counter()
    deadline = started + duration_seconds if duration_seconds is not None else None
    lock = threading.Lock()
    scheduled = 0
    results: list[dict] = []

    def worker() -> None:
        nonlocal scheduled
        while True:
            with lock:
                if scheduled >= request_limit or (deadline is not None and time.perf_counter() >= deadline):
                    return
                scheduled += 1
                sequence = scheduled
            item = request_once(gateway_id, phase, sequence, run_id, timeout)
            with lock:
                results.append(item)

    with ThreadPoolExecutor(max_workers=concurrency, thread_name_prefix=f"s3-{phase}") as pool:
        futures = [pool.submit(worker) for _ in range(concurrency)]
        for future in futures:
            future.result()
    return sorted(results, key=lambda item: item["sequence"]), time.perf_counter() - started


def summarize(requests: list[dict], elapsed_seconds: float) -> dict:
    latencies = [item["client_wall_ms"] for item in requests]
    statuses = Counter(str(item["http_status"]) if item["http_status"] is not None else "transport_error" for item in requests)
    error_kinds = Counter(item["error"]["kind"] for item in requests if item.get("error"))
    successful = sum(1 for item in requests if item.get("http_status") is not None and 200 <= item["http_status"] < 300)
    total = len(requests)
    return {
        "total_requests": total,
        "successful_requests": successful,
        "failed_requests": total - successful,
        "http_status_counts": dict(sorted(statuses.items())),
        "error_counts": dict(sorted(error_kinds.items())),
        "latency_ms": {
            "p50": benchmark.percentile(latencies, 0.50),
            "p95": benchmark.percentile(latencies, 0.95),
            "p99": benchmark.percentile(latencies, 0.99),
        },
        "elapsed_seconds": round(elapsed_seconds, 3),
        "throughput_requests_per_second": round(total / elapsed_seconds, 3) if elapsed_seconds > 0 else 0,
    }


def attach_evidence(requests: list[dict], gateway_id: str, replicas: list[dict]) -> list[str]:
    gateway_errors = s1.attach_log_evidence(requests, gateway_id, replicas)
    return gateway_errors


def distribution(requests: list[dict]) -> dict[str, int]:
    counts = Counter(
        item["replica_that_handled"] for item in requests
        if item.get("http_status") is not None and 200 <= item["http_status"] < 300 and item.get("replica_that_handled")
    )
    return dict(sorted(counts.items()))


def all_successful(requests: list[dict]) -> bool:
    return bool(requests) and all(item.get("http_status") == 200 and item.get("error") is None for item in requests)


def report_path(args: argparse.Namespace) -> Path:
    if args.output:
        path = args.output if args.output.is_absolute() else ROOT / args.output
    else:
        stamp = dt.datetime.now(s1.ART).strftime("%Y%m%d-%H%M%S-%f")
        path = ROOT / "output" / "s3" / f"load-resilience-{stamp}.json"
    path = path.resolve()
    if ROOT not in path.parents:
        raise ValueError("Report path must remain inside the MELT repository")
    if ROOT / "output" / "s1" in path.parents:
        raise ValueError("S3 reports must not be written under output/s1")
    if path.exists():
        raise ValueError(f"Refusing to overwrite existing report: {path}")
    return path


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true", help="run a local read-only GET workload against melt-s1")
    parser.add_argument("--inject-failure", action="store_true", help="also stop and restore one guarded melt-s1 Catalog replica")
    mode = parser.add_mutually_exclusive_group()
    mode.add_argument("--requests", type=int, help="maximum requests per phase; default 40")
    mode.add_argument("--duration-seconds", type=float, help="run each phase until this duration or --max-requests is reached")
    parser.add_argument("--max-requests", type=int, default=5000, help="request cap per phase for duration mode (default: 5000)")
    parser.add_argument("--concurrency", type=int, default=2, help="parallel workers (1-10; default: 2)")
    parser.add_argument("--timeout", type=float, default=5.0, help="per-request HTTP timeout in seconds (1-30; default: 5)")
    parser.add_argument("--recovery-timeout", type=float, default=180.0, help="maximum replica recovery wait (5-180; default: 180)")
    parser.add_argument("--output", type=Path, help="new JSON report path; default: output/s3/load-resilience-<timestamp>.json")
    args = parser.parse_args()

    if not args.execute:
        parser.error("Pass --execute to start a workload; this script never runs a load implicitly")
    if args.inject_failure and args.duration_seconds is not None and args.duration_seconds < 1:
        parser.error("Failover load duration must be at least 1 second")
    if args.requests is None and args.duration_seconds is None:
        args.requests = 40
    if args.requests is not None and not 1 <= args.requests <= 10000:
        parser.error("--requests must be between 1 and 10000")
    if args.duration_seconds is not None and not 1 <= args.duration_seconds <= 300:
        parser.error("--duration-seconds must be between 1 and 300")
    if not 1 <= args.max_requests <= 10000 or not 1 <= args.concurrency <= 10:
        parser.error("Limits: --max-requests 1-10000 and --concurrency 1-10")
    if not 1 <= args.timeout <= 30 or not 5 <= args.recovery_timeout <= 180:
        parser.error("Limits: --timeout 1-30 seconds and --recovery-timeout 5-180 seconds")
    request_limit = args.requests if args.requests is not None else args.max_requests
    if args.inject_failure and request_limit < 2:
        parser.error("Failover comparison needs at least two requests per phase to observe both replicas in baseline")

    try:
        path = report_path(args)
    except ValueError as exc:
        parser.error(str(exc))
    path.parent.mkdir(parents=True, exist_ok=True)
    run_id = uuid.uuid4().hex[:10]
    report = {
        "schema": "melt-s3-load-resilience/v1",
        "started_at": timestamp(),
        "finished_at": None,
        "branch": None,
        "commit": None,
        "project": PROJECT,
        "compose_file": "compose.s1.yaml",
        "endpoint_from_gateway_container": ENDPOINT,
        "load_profile": {
            "mode": "duration" if args.duration_seconds is not None else "request_count",
            "request_limit_per_phase": request_limit,
            "duration_limit_seconds": args.duration_seconds,
            "concurrency": args.concurrency,
            "timeout_seconds": args.timeout,
            "failure_injection_requested": args.inject_failure,
        },
        "measurement_method": "Docker exec + BusyBox wget from the isolated S1 Gateway; client latency includes Docker exec startup and HTTP round trip.",
        "docker_context": None,
        "network": None,
        "replicas": [],
        "target_replica": None,
        "phases": {},
        "comparison": None,
        "errors": [],
        "warnings": [],
        "restoration": {"attempted": False, "restored": False, "error": None},
        "safety_checks": {},
        "overall_passed": False,
        "report_path": str(path),
    }
    write_report(path, report)

    target_id: str | None = None
    target_may_be_stopped = False
    gateway_id: str | None = None
    replica_summaries: list[dict] = []
    initial_containers: list[dict] = []
    failover_started = False
    aborted = False
    target_stopped_at: float | None = None
    target_restart_at: float | None = None
    original_sigterm = signal.getsignal(signal.SIGTERM)

    def handle_sigterm(_signum, _frame):
        raise KeyboardInterrupt("SIGTERM received")

    signal.signal(signal.SIGTERM, handle_sigterm)
    try:
        report["branch"] = s1.git("rev-parse", "--abbrev-ref", "HEAD")
        report["commit"] = s1.git("rev-parse", "HEAD")
        report["working_tree_changes"] = s1.git("status", "--short").splitlines()
        context = s1.verify_local_docker_context()
        _, network_name = s1.verify_effective_compose()
        initial_containers = s1.project_containers()
        raw_replicas, gateway = s1.verify_project_shape(initial_containers, network_name)
        replica_summaries = [s1.container_summary(item) for item in raw_replicas]
        gateway_id = gateway["Id"]
        report["docker_context"] = context
        report["network"] = network_name
        report["replicas"] = replica_summaries
        report["safety_checks"] = {
            "local_docker_desktop_context": True,
            "compose_project_exactly_melt_s1": True,
            "internal_network_verified": True,
            "catalog_mongodb_uri_local": True,
            "two_catalog_replicas_healthy": True,
            "gateway_running": True,
            "non_target_services_guarded": True,
        }
        write_report(path, report)

        baseline, elapsed = run_phase(gateway_id, "normal", run_id, args.concurrency, args.timeout, request_limit, args.duration_seconds)
        attach_evidence(baseline, gateway_id, replica_summaries)
        report["phases"]["normal"] = {"summary": summarize(baseline, elapsed), "requests": baseline}
        report["distributions"] = {"normal": distribution(baseline)}
        write_report(path, report)

        if args.inject_failure:
            normal_replicas_seen = set(report["distributions"]["normal"])
            baseline_guard = (
                all_successful(baseline)
                and all(item.get("gateway_log_found") and item.get("catalog_log_found") for item in baseline)
                and len(normal_replicas_seen) == 2
            )
            if not baseline_guard:
                raise s1.SafetyError("Normal workload must return HTTP 200 and reach both replicas with correlated logs before failure injection")

            context = s1.verify_local_docker_context(expected_name=report["docker_context"]["name"])
            report["docker_context"] = context
            fresh_replicas, _ = s1.verify_project_shape(s1.project_containers(), network_name)
            target = fresh_replicas[-1]
            target_id = target["Id"]
            target_raw = s1.inspect_container(target_id)
            target_labels = s1.labels_of(target_raw)
            if target_labels.get("com.docker.compose.project") != PROJECT or target_labels.get("com.docker.compose.service") != SERVICE:
                raise s1.SafetyError("Selected target labels do not match melt-s1/catalogo-profesionales")
            peer = next(item for item in fresh_replicas if item["Id"] != target_id)
            peer_state = peer.get("State", {})
            if not peer_state.get("Running") or (peer_state.get("Health") or {}).get("Status") != "healthy":
                raise s1.SafetyError("Remaining replica is not healthy; refusing controlled failure")
            report["target_replica"] = s1.container_summary(target)
            report["healthy_replica_during_failure"] = s1.container_summary(peer)

            # Mark before stop so cleanup also runs if Docker returns an error mid-stop.
            target_may_be_stopped = True
            s1.docker("stop", "--time", "10", target_id, timeout=20)
            stopped = s1.inspect_container(target_id)
            if stopped.get("State", {}).get("Running"):
                raise s1.SafetyError("Docker stop returned while the selected replica is still running")
            target_stopped_at = time.monotonic()
            failover_started = True
            current_peer = s1.inspect_container(peer["Id"])
            current_peer_state = current_peer.get("State", {})
            if not current_peer_state.get("Running") or (current_peer_state.get("Health") or {}).get("Status") != "healthy":
                raise s1.SafetyError("Peer replica is not healthy after stopping the target")
            if not s1.check_non_target_unchanged(initial_containers, target_id):
                raise s1.SafetyError("A non-target melt-s1 container changed state during failure phase")

            failed_phase, failed_elapsed = run_phase(gateway_id, "failure", run_id, args.concurrency, args.timeout, request_limit, args.duration_seconds)
            attach_evidence(failed_phase, gateway_id, replica_summaries)
            report["phases"]["failure"] = {"summary": summarize(failed_phase, failed_elapsed), "requests": failed_phase}
            report["distributions"]["failure"] = distribution(failed_phase)
            report["safety_checks"]["only_selected_catalog_replica_stopped"] = True
            write_report(path, report)

    except BaseException as exc:
        aborted = isinstance(exc, (KeyboardInterrupt, SystemExit))
        report["errors"].append({"phase": "setup-or-load", "type": type(exc).__name__, "message": str(exc)[:1000]})
    finally:
        if target_id and target_may_be_stopped:
            report["restoration"]["attempted"] = True
            try:
                current = s1.inspect_container(target_id)
                labels = s1.labels_of(current)
                if labels.get("com.docker.compose.project") != PROJECT or labels.get("com.docker.compose.service") != SERVICE:
                    raise s1.SafetyError("Restoration refused because target labels no longer match melt-s1/catalogo-profesionales")
                if not current.get("State", {}).get("Running"):
                    target_restart_at = time.monotonic()
                    s1.docker("start", target_id, timeout=20)
                restored, waited, last_state = s1.wait_healthy(target_id, args.recovery_timeout)
                report["restoration"].update({"restored": restored, "wait_seconds": round(waited, 3), "last_state": last_state})
                if target_stopped_at is not None and restored:
                    report["restoration"]["healthy_seconds_from_stop"] = round(time.monotonic() - target_stopped_at, 3)
                if target_restart_at is not None and restored:
                    report["restoration"]["healthy_seconds_from_restart"] = round(time.monotonic() - target_restart_at, 3)
                if not restored:
                    raise RuntimeError(f"Replica did not become healthy; last state: {last_state}")
            except BaseException as exc:
                report["restoration"]["error"] = f"{type(exc).__name__}: {str(exc)[:1000]}"
                report["errors"].append({"phase": "restoration", "type": type(exc).__name__, "message": str(exc)[:1000]})
        signal.signal(signal.SIGTERM, original_sigterm)

    if failover_started and report["restoration"].get("restored") and gateway_id and not aborted:
        # The S1 Gateway caches Docker DNS for five seconds; wait before measuring recovery.
        time.sleep(6)
        try:
            recovery, elapsed = run_phase(gateway_id, "recovery", run_id, args.concurrency, args.timeout, request_limit, args.duration_seconds)
            attach_evidence(recovery, gateway_id, replica_summaries)
            report["phases"]["recovery"] = {"summary": summarize(recovery, elapsed), "requests": recovery}
            report.setdefault("distributions", {})["recovery"] = distribution(recovery)
        except BaseException as exc:
            report["errors"].append({"phase": "recovery", "type": type(exc).__name__, "message": str(exc)[:1000]})

    if gateway_id:
        try:
            _, gateway_errors = s1.gateway_events(gateway_id)
            request_ids = {
                request["request_id"]
                for phase in report["phases"].values()
                for request in phase.get("requests", [])
            }
            for line in gateway_errors[-100:]:
                if s1.is_unrelated_upstream_warning(line, request_ids):
                    report["warnings"].append(line)
                else:
                    report["errors"].append({"phase": "gateway-log", "message": line})
        except Exception as exc:
            report["errors"].append({"phase": "gateway-log-collection", "type": type(exc).__name__, "message": str(exc)[:500]})

    if failover_started and target_id:
        try:
            report["safety_checks"]["non_target_containers_unchanged"] = s1.check_non_target_unchanged(initial_containers, target_id)
            report["safety_checks"]["both_catalog_replicas_healthy_after_recovery"] = all(s1.verify_replicas_healthy_after_recovery(replica_summaries).values())
        except Exception as exc:
            report["errors"].append({"phase": "post-recovery-safety", "type": type(exc).__name__, "message": str(exc)[:500]})

    phase_data = {name: data for name, data in report["phases"].items()}
    for name, data in phase_data.items():
        summary = data["summary"]
        summary["all_http_200"] = all_successful(data["requests"])
        summary["correlation_complete"] = bool(data["requests"]) and all(
            request.get("gateway_log_found") and request.get("catalog_log_found") and request.get("replica_that_handled")
            for request in data["requests"]
        )
        for request in data["requests"]:
            if request.get("error"):
                report["errors"].append({
                    "phase": name,
                    "request_id": request["request_id"],
                    "http_status": request.get("http_status"),
                    **request["error"],
                })

    normal = report["phases"].get("normal", {}).get("summary")
    failed = report["phases"].get("failure", {}).get("summary")
    if normal and failed:
        baseline_p95 = normal["latency_ms"]["p95"]
        failure_p95 = failed["latency_ms"]["p95"]
        baseline_rps = normal["throughput_requests_per_second"]
        failure_rps = failed["throughput_requests_per_second"]
        report["comparison"] = {
            "normal_vs_failure": {
                "p95_delta_ms": round(failure_p95 - baseline_p95, 3) if baseline_p95 is not None and failure_p95 is not None else None,
                "p95_change_percent": round((failure_p95 - baseline_p95) * 100 / baseline_p95, 2) if baseline_p95 not in (None, 0) and failure_p95 is not None else None,
                "throughput_delta_requests_per_second": round(failure_rps - baseline_rps, 3),
                "failed_requests_delta": failed["failed_requests"] - normal["failed_requests"],
            }
        }

    errors_absent = all(summary["failed_requests"] == 0 for summary in [item["summary"] for item in phase_data.values()])
    phase_success = bool(normal and normal["all_http_200"])
    if args.inject_failure:
        distributions = report.get("distributions", {})
        target_name = (report.get("target_replica") or {}).get("name")
        peer_name = (report.get("healthy_replica_during_failure") or {}).get("name")
        phase_success = (
            phase_success
            and len(distributions.get("normal", {})) == 2
            and bool(distributions.get("failure"))
            and set(distributions.get("failure", {})) == {peer_name}
            and report.get("restoration", {}).get("restored") is True
            and report.get("safety_checks", {}).get("non_target_containers_unchanged") is True
            and report.get("safety_checks", {}).get("both_catalog_replicas_healthy_after_recovery") is True
            and all_successful(report.get("phases", {}).get("failure", {}).get("requests", []))
            and all_successful(report.get("phases", {}).get("recovery", {}).get("requests", []))
            and len(report.get("distributions", {}).get("recovery", {})) == 2
            and target_name in report.get("distributions", {}).get("recovery", {})
        )
    report["overall_passed"] = bool(phase_success and errors_absent and not report["errors"])
    report["finished_at"] = timestamp()
    write_report(path, report)
    print(json.dumps({
        "report": str(path),
        "commit": report.get("commit"),
        "phase_summaries": {name: item["summary"] for name, item in report["phases"].items()},
        "comparison": report.get("comparison"),
        "restoration": report["restoration"],
        "overall_passed": report["overall_passed"],
        "errors": report["errors"],
        "warnings": report["warnings"],
    }, ensure_ascii=False, indent=2))
    return 130 if aborted else (0 if report["overall_passed"] else 1)


if __name__ == "__main__":
    sys.exit(main())
