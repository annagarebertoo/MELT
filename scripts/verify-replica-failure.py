"""Controlled single-replica outage. Requires explicit authorization, then --execute-authorized.

Stops and starts only melt-catalogo-profesionales-2, restoring it in finally.
Never deletes containers, records or volumes and never changes configuration.
"""

import argparse
import datetime
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import time
import urllib.request
import urllib.error
import uuid

ROOT = Path(__file__).resolve().parents[1]
TARGET = "melt-catalogo-profesionales-2"
sys.dont_write_bytecode = True
spec = importlib.util.spec_from_file_location("d3", ROOT / "scripts/verify-persistence.py")
d3 = importlib.util.module_from_spec(spec)
spec.loader.exec_module(d3)


def command(*args):
    return subprocess.check_output(args, cwd=ROOT, text=True, encoding="utf-8", timeout=45).strip()


def inspect(name):
    raw = json.loads(command("docker", "inspect", name))[0]
    return {"id": raw["Id"], "running": raw["State"]["Running"], "started_at": raw["State"]["StartedAt"],
            "service": raw["Config"]["Labels"].get("com.docker.compose.service"),
            "project": raw["Config"]["Labels"].get("com.docker.compose.project")}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute-authorized", action="store_true")
    parser.add_argument("--output", type=Path, required=True)
    args = parser.parse_args()
    if not args.execute_authorized:
        parser.error("Obtain authorization before using --execute-authorized")
    original = inspect(TARGET)
    assert original["running"] and original["service"] == "catalogo-profesionales" and original["project"] == "melt"
    assert inspect("melt-catalogo-profesionales-1")["running"]
    protected = {name: inspect(name) for name in ["melt-gateway-1", "melt-mongodb-1", "melt-jaeger-1"]}
    snapshot = d3.snapshot()
    evidence = {"started_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
                "base_commit": command("git", "rev-parse", "HEAD"), "target": TARGET,
                "authorization": "User explicitly authorized single-replica outage and restoration in this session.",
                "before": original, "requests": [], "errors": [], "restored": False}

    def requests(phase):
        for _ in range(12):
            rid = "sofia-fallo-" + uuid.uuid4().hex
            req = urllib.request.Request("http://localhost:8080/api/v1/tratamientos?page=1&limit=10", headers={"X-Request-ID": rid})
            started = time.monotonic()
            status = None
            valid_json = False
            try:
                with urllib.request.urlopen(req, timeout=10) as response:
                    status = response.status
                    valid_json = isinstance(json.load(response).get("tratamientos"), list)
            except urllib.error.HTTPError as error:
                status = error.code
            except (OSError, ValueError) as error:
                evidence["errors"].append({"phase": phase, "error_type": type(error).__name__})
            evidence["requests"].append({"phase": phase, "request_id": rid, "status": status,
                                         "valid_list": valid_json, "duration_ms": round((time.monotonic() - started) * 1000, 3)})

    try:
        command("docker", "stop", "--time", "10", TARGET)
        evidence["stopped_at_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        evidence["while_stopped"] = inspect(TARGET)
        assert not evidence["while_stopped"]["running"]
        requests("one_replica_stopped")
    except Exception as error:
        evidence["errors"].append({"phase": "outage", "error_type": type(error).__name__})
    finally:
        command("docker", "start", TARGET)
        evidence["restart_requested_at_utc"] = datetime.datetime.now(datetime.timezone.utc).isoformat()
        for _ in range(30):
            try:
                health = json.loads(command("docker", "compose", "exec", "-T", "gateway", "wget", "-qO-", "-T", "2", "http://" + TARGET + ":8081/health"))
                if health.get("estado") == "ok":
                    evidence["restored"] = True
                    break
            except subprocess.CalledProcessError:
                pass
            time.sleep(1)

    if evidence["restored"]:
        # Docker DNS is cached for five seconds by the existing gateway config.
        time.sleep(6)
        requests("after_recovery")
    evidence["after"] = inspect(TARGET)
    evidence["same_container"] = evidence["after"]["id"] == original["id"]
    evidence["protected_containers_unchanged"] = all(inspect(name) == state for name, state in protected.items())
    evidence["mongodb_snapshot_unchanged"] = d3.snapshot() == snapshot
    logs = command("docker", "compose", "logs", "--no-color", "--no-log-prefix", "--since", "5m", "gateway", "catalogo-profesionales")
    parsed = []
    for line in logs.splitlines():
        try:
            value = json.loads(line)
            if isinstance(value, dict):
                parsed.append(value)
        except ValueError:
            pass
    for request in evidence["requests"]:
        request["logs"] = [v for v in parsed if v.get("request_id") == request["request_id"] and "method" in v]
    evidence["phase_instances"] = {phase: sorted({log["instance"] for req in evidence["requests"] if req["phase"] == phase
                                                  for log in req["logs"] if log.get("service") == "catalogo-profesionales"})
                                   for phase in ["one_replica_stopped", "after_recovery"]}
    evidence["all_requests_succeeded"] = all(r["status"] == 200 and r["valid_list"] for r in evidence["requests"]) and len(evidence["requests"]) == 24
    evidence["limitations"] = ["No outage of MongoDB, Jaeger, gateway or all replicas.",
                               "No proof of explicit passive-ejection timer or exhausted proxy timeouts.",
                               "Recovery includes a new start of the same replica; not proof of crash recovery durability."]
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps({key: evidence[key] for key in ["restored", "same_container", "protected_containers_unchanged", "mongodb_snapshot_unchanged", "all_requests_succeeded", "phase_instances", "errors"]}, indent=2))
    if not (evidence["restored"] and evidence["all_requests_succeeded"] and evidence["mongodb_snapshot_unchanged"] and evidence["protected_containers_unchanged"] and len(evidence["phase_instances"]["after_recovery"]) == 2):
        sys.exit(1)


if __name__ == "__main__":
    main()
