"""D5: GET requests and read-only Docker inspection; no service lifecycle changes."""

import argparse
import datetime
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("d11", ROOT / "scripts/verify-observability.py")
d11 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(d11)


def command(*args):
    return subprocess.run(args, cwd=ROOT, check=True, capture_output=True,
                          text=True, encoding="utf-8", timeout=60).stdout.strip()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway", default="http://localhost:8080")
    parser.add_argument("--jaeger", default="http://localhost:16686")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    configuration = d11.docker("compose", "exec", "-T", "gateway", "cat", "/etc/nginx/conf.d/default.conf")
    normalized = lambda text: text.replace("\r\n", "\n").strip()
    d11.check(normalized(configuration) == normalized((ROOT / "infra/nginx/default.conf").read_text(encoding="utf-8")),
              "Container configuration differs from repository")
    d11.docker("compose", "exec", "-T", "gateway", "nginx", "-T")
    timeouts = {name: re.search(r"\b" + name + r"\s+([^;]+);", configuration).group(1)
                for name in ["proxy_connect_timeout", "proxy_send_timeout", "proxy_read_timeout"]}
    d11.check(timeouts == {"proxy_connect_timeout": "2s", "proxy_send_timeout": "5s", "proxy_read_timeout": "5s"},
              "Unexpected gateway timeout configuration")
    evidence = {
        "captured_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "branch": command("git", "branch", "--show-current"),
        "base_commit": command("git", "rev-parse", "HEAD"),
        "read_only": True,
        "nginx": {"configuration_test": "passed", "container_file_matches_repository": True,
                  "configuration_sha256": hashlib.sha256(normalized(configuration).encode()).hexdigest(),
                  "timeouts": timeouts,
                  "explicit_proxy_next_upstream_directives": re.findall(r"\bproxy_next_upstream\w*\s+[^;]+;", configuration),
                  "timeout_expiry_or_failover_tested": False},
        "requests": [],
    }
    missing = "d5-absent-" + uuid.uuid4().hex
    cases = [
        ("health", "/api/v1/catalogo/health", 200, None),
        ("list", "/api/v1/catalogo/tratamientos", 200, None),
        ("detail", "/api/v1/catalogo/tratamientos/test-001", 200, None),
        ("invalid_limit", "/api/v1/catalogo/tratamientos?limit=0", 400, "INVALID_PARAMETER"),
        ("invalid_sort", "/api/v1/catalogo/tratamientos?sort=d5-invalid", 400, "INVALID_PARAMETER"),
        ("missing_treatment", "/api/v1/catalogo/tratamientos/" + missing, 404, "TRATAMIENTO_NOT_FOUND"),
    ]
    for label, path, expected_status, expected_error in cases:
        request_id = "sofia-d5-" + uuid.uuid4().hex
        trace_id = uuid.uuid4().hex
        headers = {"X-Request-ID": request_id, "traceparent": "00-" + trace_id + "-" + uuid.uuid4().hex[:16] + "-01"}
        status, response_headers, body = d11.get_json(args.gateway.rstrip("/") + path, headers)
        d11.check(status == expected_status, label + ": unexpected HTTP status")
        d11.check(d11.response_header(response_headers, "X-Request-ID") == request_id, "Request ID lost")
        d11.check(d11.response_header(response_headers, "X-Trace-ID") == trace_id, "Trace context lost")
        if expected_error:
            d11.check(body.get("error", {}).get("code") == expected_error, "Unexpected application error")
        elif label == "health":
            d11.check(body.get("estado") == "ok", "MongoDB health failed")
        elif label == "detail":
            d11.check(body.get("id") == "test-001", "Wrong treatment")
        else:
            d11.check(isinstance(body.get("tratamientos"), list), "Invalid list response")
        deadline = time.monotonic() + 5
        while True:
            records = [r for r in d11.read_logs() if r.get("request_id") == request_id and "method" in r]
            if {r.get("service") for r in records} >= {"melt-gateway", "catalogo-profesionales"}:
                break
            d11.check(time.monotonic() < deadline, "Correlated HTTP logs missing")
            time.sleep(0.2)
        d11.check(all(r.get("trace_id") == trace_id and r.get("status") == status for r in records), "Log correlation mismatch")
        gateway_log = next(r for r in records if r["service"] == "melt-gateway")
        d11.check(bool(gateway_log.get("upstream")), "No actual upstream recorded")
        evidence["requests"].append({"case": label, "status": status, "error_code": expected_error,
                                     "request_id": request_id, "trace_id": trace_id,
                                     "logs": records})
        print("PASS", label, status, request_id)

    containers = [json.loads(line) for line in d11.docker("compose", "ps", "--all", "--format", "json").splitlines() if line.strip()]
    config = json.loads(d11.docker("compose", "config", "--format", "json"))
    scanned = sorted(p for p in (ROOT / "services").rglob("*") if p.is_file() and (p.suffix == ".go" or p.name == "go.mod"))
    hits = [str(p.relative_to(ROOT)).replace("\\", "/") for p in scanned
            if re.search(r"amqp|rabbitmq|PublishWithContext|ConsumeWithContext", p.read_text(encoding="utf-8"), re.I)]
    evidence["asynchronous"] = {
        "rabbitmq_declared_image": config["services"]["rabbitmq"]["image"],
        "rabbitmq_containers": [{"name": c["Name"], "state": c["State"], "health": c.get("Health")}
                                for c in containers if c["Service"] == "rabbitmq"],
        "source_scan": {"scope": "services/**/*.go and services/**/go.mod", "files_scanned": len(scanned), "matching_files": hits},
        "business_event_publication_consumption_tested": False,
    }
    regression = command(sys.executable, "-X", "utf8", "scripts/verify-observability.py",
                         "--gateway", args.gateway, "--jaeger", args.jaeger)
    evidence["d11_regression"] = regression.splitlines()
    evidence["limits"] = ["No timeout expiry, dependency failure or retry behavior provoked.",
                          "No service start, stop, restart or persistent business writes performed.",
                          "Gateway-to-service HTTP validated; business calls between microservices are not implemented.",
                          "Source scan is scoped evidence, not a broker queue/consumer inspection."]
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("PASS configuration, six HTTP cases and D11 regression")
    print("RabbitMQ containers:", evidence["asynchronous"]["rabbitmq_containers"])
    print("Messaging source matches:", hits)


if __name__ == "__main__":
    main()
