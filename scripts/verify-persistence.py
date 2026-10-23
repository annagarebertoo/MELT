"""Read-only D3 integration check. Requires existing Docker services and test-001.

Does not start/restart services, initialize indexes, seed or remove data.
Only the optional --output JSON file is written by this script.
"""

import argparse
import datetime
import hashlib
import json
from pathlib import Path
import subprocess
import urllib.request


ROOT = Path(__file__).resolve().parents[1]


def command(*args):
    return subprocess.run(args, cwd=ROOT, check=True, capture_output=True,
                          text=True, encoding="utf-8", timeout=30).stdout.strip()


def check(condition, message):
    if not condition:
        raise RuntimeError(message)


def snapshot():
    # Projection excludes MongoDB's internal identifier; only public catalog data
    # is retained. No authentication configuration or personal data is exported.
    js = """const c=db.getSiblingDB('catalogo_db').tratamientos;
    print(JSON.stringify({ping:db.adminCommand({ping:1}).ok,
      documents:c.find({}, {_id:0}).sort({id:1}).toArray(),
      indexes:c.getIndexes().map(x=>({name:x.name,key:x.key,unique:x.unique===true})),
      databases:db.adminCommand({listDatabases:1,nameOnly:true}).databases.map(x=>x.name)
    }));"""
    return json.loads(command("docker", "compose", "exec", "-T", "mongodb",
                              "mongosh", "--quiet", "--eval", js))


def digest(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, ensure_ascii=False,
                                     separators=(",", ":")).encode()).hexdigest()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway", default="http://localhost:8080")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    before = snapshot()
    check(before["ping"] == 1, "MongoDB ping failed")
    existing = [d for d in before["documents"] if d.get("id") == "test-001"]
    check(len(existing) == 1, "Expected existing test-001; no fixture will be created")
    indexes = before["indexes"]
    check(any(i["key"] == {"id": 1} and i["unique"] for i in indexes),
          "Unique treatment id index is absent")
    evidence = {
        "captured_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "branch": command("git", "branch", "--show-current"),
        "base_commit": command("git", "rev-parse", "HEAD"),
        "read_only": True,
        "mongodb": {"database": "catalogo_db", "collection": "tratamientos",
                    "ping": before["ping"], "count": len(before["documents"]),
                    "existing_treatment_id": "test-001", "indexes": indexes,
                    "databases_present": before["databases"]},
        "requests": [],
    }
    for path in ["/api/v1/catalogo/health", "/api/v1/catalogo/tratamientos/test-001",
                 "/api/v1/catalogo/tratamientos?page=1&limit=10"]:
        with urllib.request.urlopen(args.gateway.rstrip("/") + path, timeout=10) as response:
            body = json.load(response)
            check(response.status == 200, "Unexpected gateway status")
            evidence["requests"].append({"path": path, "status": response.status,
                                         "request_id": response.headers.get("X-Request-ID"),
                                         "trace_id": response.headers.get("X-Trace-ID")})
        if path.endswith("test-001"):
            check(body == existing[0], "Gateway detail differs from stored document")
            evidence["detail_matches_mongodb"] = True
            evidence["public_document_sha256"] = digest(body)
        elif "?page" in path:
            check(body["pagination"]["total"] == len(before["documents"]), "Count differs")
            stored = {d["id"]: d for d in before["documents"]}
            check(len(body["tratamientos"]) == min(10, len(stored)), "Wrong first page length")
            check(all(d == stored.get(d["id"]) for d in body["tratamientos"]),
                  "List differs from stored documents")
            evidence["list_matches_mongodb"] = True
        else:
            check(body.get("estado") == "ok", "Catálogo cannot ping MongoDB")

    mongo_id = command("docker", "compose", "ps", "-q", "mongodb")
    mounts = json.loads(command("docker", "inspect", "--format", "{{json .Mounts}}", mongo_id))
    volumes = [{"type": m["Type"], "name": m.get("Name"), "destination": m["Destination"]}
               for m in mounts if m["Destination"] == "/data/db"]
    check(len(volumes) == 1 and volumes[0]["type"] == "volume", "MongoDB data volume is absent")
    evidence["mongodb"]["data_volume"] = volumes[0]
    evidence["mongodb"]["container_started_at"] = command(
        "docker", "inspect", "--format", "{{.State.StartedAt}}", mongo_id)
    evidence["replicas"] = []
    gateway_id = command("docker", "compose", "ps", "-q", "gateway")
    for container_id in command("docker", "compose", "ps", "-q", "catalogo-profesionales").splitlines():
        name = command("docker", "inspect", "--format", "{{.Name}}", container_id).lstrip("/")
        actual = json.loads(command("docker", "exec", gateway_id, "wget", "-qO-", "-T", "5",
                                    "http://" + name + ":8081/tratamientos/test-001"))
        check(actual == existing[0], "Replica differs from MongoDB document")
        evidence["replicas"].append({"name": name, "detail_matches_mongodb": True,
                                    "started_at": command("docker", "inspect", "--format",
                                                          "{{.State.StartedAt}}", container_id)})
    check(evidence["replicas"], "No running Catálogo replica")
    after = snapshot()
    check(before == after, "MongoDB snapshot changed during read-only checks; investigate concurrent writes")
    evidence["snapshot_unchanged"] = True
    evidence["snapshot_sha256"] = digest(before)
    evidence["limits"] = [
        "No insert, delete, index modification or service restart performed.",
        "Volume attachment and repeated retrieval verified; crash/restart durability not tested.",
        "Uniqueness index inspected; duplicate-write and concurrent-write behavior not tested.",
        "No PostgreSQL, cross-service consistency or database permission isolation validated.",
    ]
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print(json.dumps(evidence, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
