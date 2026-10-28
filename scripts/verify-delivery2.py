"""Read-only Entrega 2 audit: inspection and 12 sequential GETs, not a load test."""

import argparse
import datetime
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import sys
import time
import uuid

ROOT = Path(__file__).resolve().parents[1]
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("d11", ROOT / "scripts/verify-observability.py")
d11 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(d11)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway", default="http://localhost:8080")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    config = json.loads(d11.docker("compose", "config", "--format", "json"))
    containers = [json.loads(line) for line in d11.docker("compose", "ps", "--all", "--format", "json").splitlines() if line.strip()]
    evidence = {
        "captured_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "read_only": True, "base_commit": "", "source_files": {},
        "containers": [{k: c.get(k) for k in ["Name", "Service", "State", "Health", "ID"]} for c in containers],
        "declared_services": sorted(config["services"]),
        "requests": [], "replicas": [],
    }
    import subprocess
    for key, args_git in [("branch", ["branch", "--show-current"]), ("base_commit", ["rev-parse", "HEAD"])]:
        evidence[key] = subprocess.check_output(["git", *args_git], cwd=ROOT, text=True).strip()
    for path in sorted((ROOT / "services").rglob("*.go")):
        if not path.name.endswith("_test.go"):
            content = path.read_text(encoding="utf-8")
            evidence["source_files"][path.relative_to(ROOT).as_posix()] = {
                "sha256": hashlib.sha256(path.read_bytes()).hexdigest(),
                "direct_mongo_collection_dependency": "*mongo.Collection" in content,
                "database_names": re.findall(r'\.Database\("([^\"]+)"\)', content),
                "search_cache_client_mentions": re.findall(r"opensearch|memcached", content, re.I),
            }
    nginx = d11.docker("compose", "exec", "-T", "gateway", "nginx", "-T")
    evidence["nginx"] = {
        "configuration_test": "passed",
        "timeouts": re.findall(r"proxy_(?:connect|send|read)_timeout\s+[^;]+;", nginx),
        "explicit_retry_or_active_health_directives": re.findall(r"\b(?:proxy_next_upstream\w*|health_check)\s+[^;]+;", nginx),
        "failure_injection_performed": False,
    }
    for c in containers:
        if c["Service"] != "catalogo-profesionales" or c["State"] != "running":
            continue
        health = json.loads(d11.docker("compose", "exec", "-T", "gateway", "wget", "-qO-", "-T", "5", "http://" + c["Name"] + ":8081/health"))
        d11.check(health.get("estado") == "ok", "Unhealthy replica")
        limits = json.loads(d11.docker("inspect", "--format", '{{json .HostConfig}}', c["ID"]))
        evidence["replicas"].append({"name": c["Name"], "instance": c["ID"], "health": health["estado"],
                                    "memory_limit_bytes": limits["Memory"], "nano_cpus_limit": limits["NanoCpus"]})
    d11.check(len(evidence["replicas"]) == 2, "Expected two running replicas")
    for _ in range(12):
        rid = "sofia-cierre-" + uuid.uuid4().hex
        status, headers, body = d11.get_json(args.gateway.rstrip("/") + "/api/v1/tratamientos?page=1&limit=10", {"X-Request-ID": rid})
        d11.check(status == 200 and isinstance(body.get("tratamientos"), list), "Public endpoint failed")
        d11.check(d11.response_header(headers, "X-Request-ID") == rid, "Request ID lost")
        evidence["requests"].append({"request_id": rid, "status": status})
    deadline = time.monotonic() + 5
    while True:
        logs = d11.read_logs()
        selected = {r["request_id"]: [l for l in logs if l.get("request_id") == r["request_id"] and "method" in l]
                    for r in evidence["requests"]}
        if all({l.get("service") for l in records} >= {"melt-gateway", "catalogo-profesionales"} for records in selected.values()):
            break
        d11.check(time.monotonic() < deadline, "Correlated logs missing")
        time.sleep(0.2)
    distribution = {}
    for request in evidence["requests"]:
        records = selected[request["request_id"]]
        gateway = next(r for r in records if r.get("service") == "melt-gateway")
        service = next(r for r in records if r.get("service") == "catalogo-profesionales")
        d11.check(gateway.get("trace_id") == service.get("trace_id") and bool(gateway.get("trace_id")), "Trace IDs differ")
        request.update({"upstream": gateway["upstream"], "instance": service["instance"], "trace_id": gateway["trace_id"]})
        distribution[service["instance"]] = distribution.get(service["instance"], 0) + 1
    evidence["distribution"] = distribution
    evidence["both_replicas_observed"] = len(distribution) == 2
    evidence["resource_observation"] = {
        "docker_host": json.loads(d11.docker("info", "--format", '{"cpus":{{.NCPU}},"memory_bytes":{{.MemTotal}}}')),
        "stats_single_snapshot": [json.loads(line) for line in d11.docker("stats", "--no-stream", "--format", '{{json .}}',
                                                                        *[c["ID"] for c in containers if c["State"] == "running"]).splitlines()],
        "load_test": False, "bottleneck_measured": False, "cost_estimate_available": False,
    }
    evidence["limitations"] = ["Healthy-instance distribution only; failover and timeout expiry not tested.",
                               "Resource snapshot is not capacity, cost or cache/search effectiveness evidence.",
                               "No service lifecycle operations, persistent writes or provider calls performed."]
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("PASS two healthy replicas; 12 successful correlated public GETs")
    print("Observed distribution:", json.dumps(distribution))
    print("Declared but absent:", sorted(set(config["services"]) - {c["Service"] for c in containers}))
    print("No load/failure test performed; no capacity/cost conclusion")


if __name__ == "__main__":
    main()
