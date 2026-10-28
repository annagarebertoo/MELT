"""D11 integration check: GET requests and read-only Docker/Jaeger queries only.

Run from the repository root after starting the D11 services.
No packages beyond the Python standard library are required.
"""

import argparse
import datetime
import hashlib
import json
import re
import subprocess
import time
import urllib.error
import urllib.request
import uuid
from pathlib import Path


def check(condition, message):
    if not condition:
        raise AssertionError(message)


def docker(*args):
    result = subprocess.run(
        ["docker", *args], capture_output=True, text=True, encoding="utf-8", errors="replace"
    )
    check(result.returncode == 0, "Docker query failed; check Docker Desktop and permissions")
    return result.stdout


def get_json(url, headers=None):
    request = urllib.request.Request(url, headers=headers or {})
    try:
        with urllib.request.urlopen(request, timeout=10) as response:
            return response.status, dict(response.headers), json.load(response)
    except urllib.error.HTTPError as error:
        return error.code, dict(error.headers), json.load(error)


def response_header(headers, name):
    return next((value for key, value in headers.items() if key.lower() == name.lower()), "")


def read_logs():
    output = docker(
        "compose", "logs", "--no-color", "--no-log-prefix", "--since", "5m",
        "gateway", "catalogo-profesionales",
    )
    records = []
    for line in output.splitlines():
        try:
            record = json.loads(line)
        except json.JSONDecodeError:
            continue  # Ignore container startup diagnostics and older log formats.
        if isinstance(record, dict) and "request_id" in record:
            records.append(record)
    return records


def wait_for_trace(jaeger, trace_id):
    deadline = time.monotonic() + 30
    while time.monotonic() < deadline:
        try:
            status, _, result = get_json(jaeger + "/api/traces/" + trace_id)
            if status == 200 and result.get("data"):
                trace = result["data"][0]
                processes = trace["processes"]
                services = {processes[span["processID"]]["serviceName"] for span in trace["spans"]}
                database = [span for span in trace["spans"] if tags(span).get("db.system.name", tags(span).get("db.system")) == "mongodb"]
                if {"melt-gateway", "catalogo-profesionales"} <= services and len(database) >= 2:
                    return trace
        except (OSError, ValueError):
            pass  # The exporter batches spans; a trace may not be complete yet.
        time.sleep(1)
    raise AssertionError("No complete gateway -> catalogo -> MongoDB trace within 30 seconds")


def tags(span):
    return {tag["key"]: tag["value"] for tag in span.get("tags", [])}


def parent(span):
    return next((ref["spanID"] for ref in span.get("references", []) if ref["refType"] == "CHILD_OF"), None)


def verify_trace(trace):
    spans = trace["spans"]
    processes = trace["processes"]
    gateway = next(span for span in spans if processes[span["processID"]]["serviceName"] == "melt-gateway")
    catalogo = next(
        span for span in spans
        if processes[span["processID"]]["serviceName"] == "catalogo-profesionales"
        and tags(span).get("span.kind") == "server"
    )
    check(parent(catalogo) == gateway["spanID"], "Catálogo span is not a child of NGINX")
    database = [span for span in spans if tags(span).get("db.system.name", tags(span).get("db.system")) == "mongodb"]
    check(all(parent(span) == catalogo["spanID"] for span in database), "MongoDB spans have wrong parent")
    check(all(tags(span).get("span.kind") == "client" for span in database), "MongoDB spans are not driver client spans")
    operations = {tags(span).get("db.operation.name", tags(span).get("db.operation")) for span in database}
    check({"aggregate", "find"} <= operations, "Real list request did not produce count and find spans")
    return gateway


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway", default="http://localhost:8080")
    parser.add_argument("--jaeger", default="http://localhost:16686")
    parser.add_argument("--output", type=Path, help="Optional path for a sanitized evidence JSON")
    args = parser.parse_args()
    gateway = args.gateway.rstrip("/")
    jaeger = args.jaeger.rstrip("/")
    for path in ["/health", "/api/v1/catalogo/health"]:
        status, _, _ = get_json(gateway + path)
        check(status == 200, "Health endpoint failed: " + path)

    # A marker used only in headers/query must never appear in logs or stored spans.
    private_marker = "d11-private-query-" + uuid.uuid4().hex
    suffix = uuid.uuid4().hex[:12]
    external_trace = uuid.uuid4().hex
    external_parent = uuid.uuid4().hex[:16]
    cases = [
        ("provided", "sofia-d11-" + suffix, None),
        ("generated", None, None),
        ("invalid", "invalid id", None),
        ("propagated", "sofia-context-" + suffix, "00-" + external_trace + "-" + external_parent + "-01"),
    ]
    evidence = {
        "date": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "branch": subprocess.check_output(["git", "branch", "--show-current"], text=True).strip(),
        "base_commit": subprocess.check_output(["git", "rev-parse", "HEAD"], text=True).strip(),
        "uncommitted_implementation": True,
        "implementation_sha256": {
            name: hashlib.sha256(Path(name).read_bytes()).hexdigest()
            for name in [
                "compose.yaml", "infra/nginx/nginx.conf", "infra/nginx/default.conf",
                "infra/jaeger/config.yaml", "services/catalogo-profesionales/go.mod",
                "services/catalogo-profesionales/go.sum",
                "services/catalogo-profesionales/cmd/api/main.go",
                "services/catalogo-profesionales/cmd/api/observability.go",
                "services/catalogo-profesionales/cmd/api/observability_test.go",
                "scripts/verify-observability.py",
            ]
        },
        "requests": [],
    }
    for label, supplied_id, traceparent in cases:
        headers = {"Authorization": "Bearer " + private_marker}
        if supplied_id is not None:
            headers["X-Request-ID"] = supplied_id
        if traceparent:
            headers["traceparent"] = traceparent
        status, response_headers, body = get_json(
            gateway + "/api/v1/catalogo/tratamientos?token=" + private_marker, headers
        )
        check(status == 200 and isinstance(body.get("tratamientos"), list), "List endpoint changed")
        request_id = response_header(response_headers, "X-Request-ID")
        trace_id = response_header(response_headers, "X-Trace-ID")
        check(re.fullmatch(r"[A-Za-z0-9][A-Za-z0-9_-]{0,63}", request_id), "Unsafe response request ID")
        check(re.fullmatch(r"[0-9a-f]{32}", trace_id), "Missing real trace ID")
        if label in {"provided", "propagated"}:
            check(request_id == supplied_id, "Valid supplied ID was replaced")
        else:
            check(request_id != supplied_id, "Invalid or absent ID was not generated")
        if traceparent:
            check(trace_id == external_trace, "W3C trace context was not propagated")

        trace = wait_for_trace(jaeger, trace_id)
        gateway_span = verify_trace(trace)
        if traceparent:
            check(parent(gateway_span) == external_parent, "NGINX did not preserve the external parent")
        records = [record for record in read_logs() if record["request_id"] == request_id]
        gateway_records = [r for r in records if r.get("service") == "melt-gateway"]
        catalogo_records = [r for r in records if r.get("service") == "catalogo-profesionales"]
        check(len(gateway_records) == 1 and len(catalogo_records) == 1, "Missing or duplicate correlated HTTP logs")
        for record in gateway_records + catalogo_records:
            required = {"timestamp", "level", "service", "method", "route", "status", "request_id", "trace_id", "span_id"}
            check(required <= record.keys(), "Incomplete JSON access log")
            check(record["trace_id"] == trace_id and record["status"] == 200, "Log/trace mismatch")
            check("duration_ms" in record or "duration_seconds" in record, "Missing duration")
        check(private_marker not in json.dumps(records) + json.dumps(trace), "Sensitive query/header leaked into telemetry")

        processes = trace["processes"]
        evidence["requests"].append({
            "case": label, "status": status, "request_id": request_id, "trace_id": trace_id,
            "trace_url": jaeger + "/trace/" + trace_id,
            "total_treatments": body["pagination"]["total"],
            "logs": gateway_records + catalogo_records,
            "spans": [{
                "service": processes[span["processID"]]["serviceName"],
                "operation": span["operationName"], "span_id": span["spanID"],
                "parent_span_id": parent(span),
                "kind": tags(span).get("span.kind"),
                "db_operation": tags(span).get("db.operation.name", tags(span).get("db.operation")),
            } for span in trace["spans"]],
        })
        print("PASS", label, "request_id=" + request_id, "trace_id=" + trace_id)

    containers = [json.loads(line) for line in docker("compose", "ps", "--format", "json").splitlines() if line.strip()]
    replicas = [c for c in containers if c["Service"] == "catalogo-profesionales" and c["State"] == "running"]
    check(len(replicas) == 2, "Expected two running Catálogo replicas")
    evidence["replicas"] = []
    for container in replicas:
        status = json.loads(docker("exec", "melt-gateway-1", "wget", "-qO-", "-T", "5", "http://" + container["Name"] + ":8081/health"))
        check(status.get("estado") == "ok", "Replica cannot reach MongoDB")
        evidence["replicas"].append({"name": container["Name"], "health": status["estado"]})
    print("PASS two Catálogo replicas with MongoDB healthy")
    print("PASS sensitive query and header absent from recorded request telemetry")
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
        print("Evidence:", args.output)


if __name__ == "__main__":
    main()
