"""Run a guarded Catálogo failover check against Docker project melt-s1."""

from __future__ import annotations

import argparse
import datetime as dt
import json
from pathlib import Path
import re
import signal
import subprocess
import sys
import time
import uuid


ROOT = Path(__file__).resolve().parents[1]
PROJECT = "melt-s1"
COMPOSE = ROOT / "compose.s1.yaml"
CATALOG = "catalogo-profesionales"
GATEWAY = "gateway"
EXPECTED_SERVICES = {
    "gateway", "catalogo-profesionales", "mongodb", "mongo-init",
    "opensearch", "memcached", "jaeger",
}
ENDPOINT = "http://127.0.0.1/api/v1/catalogo/tratamientos?page=1&limit=10"
CATALOG_ROUTE = "/api/v1/catalogo/tratamientos"
INSTANCE_FIELD = "instance"
ART = dt.timezone(dt.timedelta(hours=-3), "ART")
DOCKER_CONTEXT: str | None = None
WINDOWS_DOCKER_DESKTOP_ENDPOINTS = {
    "desktop-linux": "npipe:////./pipe/dockerDesktopLinuxEngine",
    "default": "npipe:////./pipe/docker_engine",
}


class SafetyError(RuntimeError):
    pass


def now_local() -> str:
    return dt.datetime.now(ART).isoformat(timespec="milliseconds")


def docker(*args: str, timeout: int = 30, check: bool = True) -> subprocess.CompletedProcess[str]:
    command = ["docker"]
    if DOCKER_CONTEXT is not None:
        command.extend(["--context", DOCKER_CONTEXT])
    command.extend(args)
    result = subprocess.run(
        command, cwd=ROOT, capture_output=True, text=True,
        encoding="utf-8", errors="replace", timeout=timeout,
    )
    if check and result.returncode != 0:
        detail = (result.stderr or result.stdout).strip()
        raise RuntimeError(f"docker {' '.join(args[:3])} failed ({result.returncode}): {detail}")
    return result


def verify_local_docker_context(expected_name: str | None = None) -> dict:
    global DOCKER_CONTEXT
    if sys.platform != "win32":
        raise SafetyError("S1 failover requires a verifiable local Docker Desktop context on Windows")
    selected = subprocess.run(
        ["docker", "context", "show"], cwd=ROOT, capture_output=True, text=True,
        encoding="utf-8", errors="replace", timeout=15,
    )
    if selected.returncode != 0:
        raise SafetyError(f"Cannot verify active Docker context: {(selected.stderr or selected.stdout).strip()}")
    context_name = selected.stdout.strip()
    expected_endpoint = WINDOWS_DOCKER_DESKTOP_ENDPOINTS.get(context_name)
    if expected_endpoint is None or (expected_name is not None and context_name != expected_name):
        raise SafetyError(f"Refusing non-local or unexpected Docker context: {context_name!r}")
    inspected = subprocess.run(
        ["docker", "context", "inspect", context_name, "--format", "{{json .Endpoints.docker}}"],
        cwd=ROOT, capture_output=True, text=True,
        encoding="utf-8", errors="replace", timeout=15,
    )
    if inspected.returncode != 0:
        raise SafetyError(f"Cannot inspect Docker context {context_name!r}: {(inspected.stderr or inspected.stdout).strip()}")
    try:
        endpoint = json.loads(inspected.stdout).get("Host")
    except (json.JSONDecodeError, AttributeError) as error:
        raise SafetyError("Docker context endpoint could not be parsed") from error
    if endpoint != expected_endpoint:
        raise SafetyError(f"Docker context endpoint is not the expected local Docker Desktop pipe: {endpoint!r}")
    DOCKER_CONTEXT = context_name
    server = docker("info", "--format", "{{.OperatingSystem}}|{{.OSType}}", timeout=15).stdout.strip()
    if server != "Docker Desktop|linux":
        raise SafetyError(f"Docker server is not local Docker Desktop Linux: {server!r}")
    return {"name": context_name, "endpoint": endpoint, "server": server}


def compose(*args: str, timeout: int = 30, check: bool = True) -> subprocess.CompletedProcess[str]:
    return docker("compose", "-p", PROJECT, "-f", str(COMPOSE), *args, timeout=timeout, check=check)


def git(*args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=ROOT, capture_output=True, text=True,
        encoding="utf-8", errors="replace", timeout=15,
    )
    if result.returncode != 0:
        raise RuntimeError(f"git {' '.join(args)} failed: {(result.stderr or result.stdout).strip()}")
    return result.stdout.strip()


def inspect_container(container_id: str) -> dict:
    raw = json.loads(docker("inspect", container_id).stdout)
    if len(raw) != 1:
        raise SafetyError(f"Expected one container for ID {container_id[:12]}")
    return raw[0]


def labels_of(raw: dict) -> dict:
    return raw.get("Config", {}).get("Labels") or {}


def verify_effective_compose() -> tuple[dict, str]:
    if not COMPOSE.is_file():
        raise SafetyError("compose.s1.yaml not found")
    source = COMPOSE.read_text(encoding="utf-8")
    if "${" in source or re.search(r"\$[A-Za-z_]", source):
        raise SafetyError("Compose contains variable interpolation; refusing possible .env inheritance")

    config = json.loads(compose("config", "--format", "json", timeout=30).stdout)
    if config.get("name") != PROJECT:
        raise SafetyError(f"Unexpected Compose project name: {config.get('name')!r}")
    services = config.get("services") or {}
    if set(services) != EXPECTED_SERVICES:
        raise SafetyError(f"Unexpected S1 service set: {sorted(services)}")
    networks = config.get("networks") or {}
    s1_network = networks.get("s1") or {}
    if s1_network.get("internal") is not True:
        raise SafetyError("The S1 network is not internal")
    for service_name, service in services.items():
        if "env_file" in service:
            raise SafetyError(f"Service {service_name} imports an env_file")
        if set(service.get("networks") or {}) != {"s1"}:
            raise SafetyError(f"Service {service_name} is connected outside the S1 network")
    catalog_env = services[CATALOG].get("environment") or {}
    if catalog_env.get("MONGODB_URI") != "mongodb://mongodb:27017/?replicaSet=rs0":
        raise SafetyError("Catálogo is not configured to use the local S1 MongoDB")
    ports = [port for service in services.values() for port in service.get("ports", [])]
    if len(ports) != 1 or ports[0].get("host_ip") != "127.0.0.1" or ports[0].get("published") != "18081":
        raise SafetyError("Unexpected host-published port in S1")
    network_name = s1_network.get("name", f"{PROJECT}_s1")
    network = json.loads(docker("network", "inspect", network_name).stdout)[0]
    if not network.get("Internal"):
        raise SafetyError("The actual Docker network is not internal")
    network_labels = network.get("Labels") or {}
    if (
        network.get("Name") != network_name
        or network_labels.get("com.docker.compose.project") != PROJECT
        or network_labels.get("com.docker.compose.network") != "s1"
    ):
        raise SafetyError("The actual S1 network is not owned by the melt-s1 Compose project")
    return config, network_name


def project_containers() -> list[dict]:
    ids = docker(
        "ps", "-aq", "--filter", f"label=com.docker.compose.project={PROJECT}"
    ).stdout.split()
    return [inspect_container(container_id) for container_id in ids]


def verify_project_shape(containers: list[dict], network_name: str) -> tuple[list[dict], dict]:
    by_service: dict[str, list[dict]] = {}
    for raw in containers:
        labels = labels_of(raw)
        if labels.get("com.docker.compose.project") != PROJECT:
            raise SafetyError("A selected container is not labelled melt-s1")
        service = labels.get("com.docker.compose.service")
        if service not in EXPECTED_SERVICES:
            raise SafetyError(f"Unexpected service in melt-s1: {service!r}")
        nets = raw.get("NetworkSettings", {}).get("Networks") or {}
        if set(nets) != {network_name}:
            raise SafetyError(f"Container {raw['Id'][:12]} is attached to an unexpected network")
        by_service.setdefault(service, []).append(raw)
    network = json.loads(docker("network", "inspect", network_name).stdout)[0]
    connected_ids = set((network.get("Containers") or {}).keys())
    running_project_ids = {raw["Id"] for raw in containers if raw.get("State", {}).get("Running")}
    if connected_ids != running_project_ids:
        raise SafetyError("The S1 network has connected containers outside the running melt-s1 project")
    replicas = by_service.get(CATALOG, [])
    if len(replicas) != 2:
        raise SafetyError(f"Expected exactly two Catálogo containers; found {len(replicas)}")
    expected_counts = {
        "gateway": 1, CATALOG: 2, "mongodb": 1, "mongo-init": 1,
        "opensearch": 1, "memcached": 1, "jaeger": 1,
    }
    for service, count in expected_counts.items():
        if len(by_service.get(service, [])) != count:
            raise SafetyError(f"Expected {count} {service} container(s); found {len(by_service.get(service, []))}")
    for service in (GATEWAY, "mongodb", "opensearch", "memcached", "jaeger"):
        if not by_service[service][0].get("State", {}).get("Running"):
            raise SafetyError(f"Required S1 service {service} is not running")
    for service in ("mongodb", "opensearch"):
        if (by_service[service][0].get("State", {}).get("Health") or {}).get("Status") != "healthy":
            raise SafetyError(f"Required S1 service {service} is not healthy")
    init_state = by_service["mongo-init"][0].get("State", {})
    if init_state.get("Running") or init_state.get("ExitCode") != 0:
        raise SafetyError("Local Mongo replica-set initialization did not complete successfully")
    for raw in replicas:
        labels = labels_of(raw)
        if labels.get("com.docker.compose.project") != PROJECT or labels.get("com.docker.compose.service") != CATALOG:
            raise SafetyError("Replica labels failed the project/service guard")
        env = raw.get("Config", {}).get("Env") or []
        if "MONGODB_URI=mongodb://mongodb:27017/?replicaSet=rs0" not in env:
            raise SafetyError(f"Replica {raw['Id'][:12]} is not using local MongoDB")
        if set((raw.get("NetworkSettings", {}).get("Networks") or {}).keys()) != {network_name}:
            raise SafetyError(f"Replica {raw['Id'][:12]} has a non-S1 network")
        if not raw.get("State", {}).get("Running") or raw.get("State", {}).get("Health", {}).get("Status") != "healthy":
            raise SafetyError(f"Replica {raw['Id'][:12]} is not running and healthy")
    gateway = by_service[GATEWAY][0]
    if not gateway.get("State", {}).get("Running"):
        raise SafetyError("Gateway is not running")
    return sorted(replicas, key=lambda item: item["Id"]), gateway


def container_summary(raw: dict) -> dict:
    labels = labels_of(raw)
    state = raw.get("State", {})
    return {
        "id": raw.get("Id"),
        "name": raw.get("Name", "").lstrip("/"),
        "project": labels.get("com.docker.compose.project"),
        "service": labels.get("com.docker.compose.service"),
        "hostname": raw.get("Config", {}).get("Hostname"),
        "running": state.get("Running"),
        "status": state.get("Status"),
        "health": (state.get("Health") or {}).get("Status"),
        "started_at": state.get("StartedAt"),
        "networks": sorted((raw.get("NetworkSettings", {}).get("Networks") or {}).keys()),
        "network_ips": {
            network: details.get("IPAddress")
            for network, details in (raw.get("NetworkSettings", {}).get("Networks") or {}).items()
        },
    }


def app_log_events(container_ids: list[str]) -> list[dict]:
    events = []
    for container_id in container_ids:
        result = docker("logs", container_id, timeout=20, check=False)
        for line in (result.stdout + "\n" + result.stderr).splitlines():
            try:
                item = json.loads(line)
            except (json.JSONDecodeError, TypeError):
                continue
            if isinstance(item, dict):
                events.append(item)
    return events


def gateway_events(gateway_id: str) -> tuple[list[dict], list[str]]:
    result = docker("logs", gateway_id, timeout=20, check=False)
    parsed, errors = [], []
    for line in (result.stdout + "\n" + result.stderr).splitlines():
        try:
            item = json.loads(line)
        except (json.JSONDecodeError, TypeError):
            if re.search(r"\[(?:error|crit|alert|emerg)\]", line, re.IGNORECASE):
                errors.append(line)
            continue
        if isinstance(item, dict):
            parsed.append(item)
    return parsed, errors


def upstream_addresses(value: str | None) -> list[str]:
    if not value:
        return []
    return [part.strip() for part in value.split(",") if part.strip() and part.strip() != "-"]


def attach_log_evidence(requests: list[dict], gateway_id: str, replica_summaries: list[dict]) -> list[str]:
    gateway_logs, gateway_errors = gateway_events(gateway_id)
    app_logs = app_log_events([item["id"] for item in replica_summaries])
    by_instance = {item.get("hostname"): item for item in replica_summaries}
    by_ip = {
        ip: item for item in replica_summaries
        for ip in item.get("network_ips", {}).values() if ip
    }
    for request in requests:
        rid = request["request_id"]
        matching_gateway = [event for event in gateway_logs if event.get("request_id") == rid and event.get("service") == "melt-gateway"]
        matching_app = [event for event in app_logs if event.get("request_id") == rid and event.get("service") == CATALOG]
        gateway_event = matching_gateway[-1] if matching_gateway else {}
        upstreams = upstream_addresses(gateway_event.get("upstream"))
        app_instances = [event.get(INSTANCE_FIELD) for event in matching_app if event.get(INSTANCE_FIELD)]
        served_by = by_instance.get(app_instances[-1]) if app_instances else None
        if served_by is None and upstreams:
            address = upstreams[-1].rsplit(":", 1)[0].strip("[]")
            served_by = by_ip.get(address)
        request.update({
            "http_status": gateway_event.get("status", request.get("http_status")),
            "gateway_duration_ms": round(float(gateway_event["duration_seconds"]) * 1000, 3)
            if gateway_event.get("duration_seconds") is not None else None,
            "upstream": upstreams,
            "catalog_instances": app_instances,
            "replica_that_handled": served_by.get("name") if served_by else None,
            "gateway_log_found": bool(matching_gateway),
            "catalog_log_found": bool(matching_app),
        })
    return gateway_errors


def send_get(gateway_id: str, phase: str, sequence: int, run_id: str) -> dict:
    request_id = f"s1-{run_id}-{phase}-{sequence:03d}"
    start = time.monotonic()
    result = docker(
        "exec", gateway_id, "wget", "-S", "-q", "-O", "/dev/null", "-T", "10",
        "--header", f"X-Request-ID: {request_id}", ENDPOINT, timeout=20, check=False,
    )
    elapsed_ms = (time.monotonic() - start) * 1000
    text = result.stdout + "\n" + result.stderr
    matches = re.findall(r"HTTP/\d(?:\.\d)?\s+(\d{3})", text)
    status = int(matches[-1]) if matches else None
    return {
        "phase": phase,
        "sequence": sequence,
        "request_id": request_id,
        "http_status": status,
        "probe_wall_ms": round(elapsed_ms, 3),
        "wget_exit_code": result.returncode,
        "probe_error": text.strip()[-500:] if result.returncode != 0 and not status else None,
    }


def wait_healthy(container_id: str, timeout: float) -> tuple[bool, float, str | None]:
    start = time.monotonic()
    deadline = start + timeout
    last_state = None
    while time.monotonic() < deadline:
        raw = inspect_container(container_id)
        labels = labels_of(raw)
        if labels.get("com.docker.compose.project") != PROJECT or labels.get("com.docker.compose.service") != CATALOG:
            raise SafetyError("Replica labels changed during recovery; refusing further actions")
        state = raw.get("State", {})
        last_state = f"{state.get('Status')} / {(state.get('Health') or {}).get('Status')}"
        if state.get("Running") and (state.get("Health") or {}).get("Status") == "healthy":
            return True, time.monotonic() - start, last_state
        time.sleep(0.5)
    return False, time.monotonic() - start, last_state


def check_non_target_unchanged(before: list[dict], target_id: str) -> bool:
    current = {item["Id"]: item for item in project_containers()}
    for original in before:
        if original["Id"] == target_id:
            continue
        raw = current.get(original["Id"])
        if raw is None or raw.get("State", {}).get("Running") != original.get("State", {}).get("Running"):
            return False
        original_service = labels_of(original).get("com.docker.compose.service")
        if labels_of(raw).get("com.docker.compose.service") != original_service:
            return False
    return True


def verify_replicas_healthy_after_recovery(replica_summaries: list[dict]) -> dict[str, bool]:
    health = {}
    for replica in replica_summaries:
        raw = inspect_container(replica["id"])
        labels = labels_of(raw)
        if labels.get("com.docker.compose.project") != PROJECT or labels.get("com.docker.compose.service") != CATALOG:
            raise SafetyError(f"Replica labels failed post-recovery validation: {replica['id'][:12]}")
        state = raw.get("State", {})
        health[replica["name"]] = bool(
            state.get("Running") and (state.get("Health") or {}).get("Status") == "healthy"
        )
    return health


def is_unrelated_upstream_warning(line: str, request_ids: set[str]) -> bool:
    if CATALOG_ROUTE in line or any(request_id in line for request_id in request_ids):
        return False
    return bool(re.search(r"\b(?:auth|reservas|pagos|beneficios)(?:_backend)?\b", line, re.IGNORECASE))


def balanced_distribution(distribution: dict[str, int]) -> bool:
    counts = list(distribution.values())
    return bool(counts) and min(counts) > 0 and max(counts) - min(counts) <= 1


def write_report(path: Path, report: dict) -> None:
    path.write_text(json.dumps(report, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--execute", action="store_true", help="confirm the guarded stop/start of one melt-s1 Catálogo replica")
    parser.add_argument("--requests", type=int, default=20, help="GETs in each phase (default: 20)")
    parser.add_argument("--recovery-timeout", type=float, default=180.0, help="Maximum seconds to observe recovery and complete the post-restart sample (default: 180)")
    parser.add_argument("--output", type=Path, help="Report path inside the repository; default: output/s1/failover-<timestamp>.json")
    args = parser.parse_args()
    if not args.execute:
        parser.error("Pass --execute to authorize the guarded local replica stop/start")
    if not 1 <= args.requests <= 50 or not 5 <= args.recovery_timeout <= 180:
        parser.error("Use 1-50 requests per phase and a 5-180 second recovery timeout")

    local_stamp = dt.datetime.now(ART).strftime("%Y%m%d-%H%M%S-%f")
    report_path = args.output or (ROOT / "output" / "s1" / f"failover-{local_stamp}.json")
    if not report_path.is_absolute():
        report_path = ROOT / report_path
    report_path = report_path.resolve()
    if ROOT not in report_path.parents:
        parser.error("Report path must be inside the MELT repository")
    report_path.parent.mkdir(parents=True, exist_ok=True)

    run_id = uuid.uuid4().hex[:10]
    report = {
        "schema": "melt-s1-failover/v1",
        "started_at": now_local(),
        "finished_at": None,
        "repository": str(ROOT),
        "branch": None,
        "commit": None,
        "working_tree_changes": None,
        "project": PROJECT,
        "compose_file": str(COMPOSE.relative_to(ROOT)),
        "network": None,
        "endpoint_from_gateway_container": ENDPOINT,
        "request_count_target_per_phase": args.requests,
        "replicas": [],
        "target_replica": None,
        "healthy_replica_during_outage": None,
        "phases": {"baseline": [], "one_replica_stopped": [], "recovery": []},
        "recovery": {},
        "continuity_passed": False,
        "recovery_passed": False,
        "overall_passed": False,
        "errors": [],
        "gateway_error_log_lines": [],
        "gateway_warnings": [],
        "safety_checks": {},
        "post_recovery_replica_health": {},
        "cleanup": {"attempted": False, "restored": False, "error": None},
        "report_path": str(report_path),
        "limitations": [
            "All HTTP probes run inside the isolated Gateway container because the internal network does not publish port 18081 to the Windows host.",
            "The test stops and starts only one labelled Catálogo container; MongoDB, OpenSearch, Memcached, Jaeger, and Gateway remain running.",
            "This is a sequential read-only failover check, not a load test or a test of database/provider failure.",
        ],
    }
    write_report(report_path, report)
    target_id: str | None = None
    target_may_be_stopped = False
    replica_summaries: list[dict] = []
    gateway_id: str | None = None
    initial_project_containers: list[dict] = []
    target_stopped_monotonic: float | None = None
    target_restart_monotonic: float | None = None
    unexpected: list[str] = []

    def record_error(where: str, error: BaseException) -> None:
        report["errors"].append({"at": now_local(), "phase": where, "type": type(error).__name__, "message": str(error)[:1000]})
        write_report(report_path, report)

    original_sigterm = signal.getsignal(signal.SIGTERM)

    def handle_sigterm(_signum, _frame):
        raise KeyboardInterrupt("SIGTERM received")

    signal.signal(signal.SIGTERM, handle_sigterm)
    try:
        report["branch"] = git("rev-parse", "--abbrev-ref", "HEAD")
        report["commit"] = git("rev-parse", "HEAD")
        report["working_tree_changes"] = git("status", "--short").splitlines()
        docker_context = verify_local_docker_context()
        report["docker_context"] = docker_context
        _, network_name = verify_effective_compose()
        report["network"] = network_name
        initial_project_containers = project_containers()
        replicas, gateway = verify_project_shape(initial_project_containers, network_name)
        replica_summaries = [container_summary(raw) for raw in replicas]
        report["replicas"] = replica_summaries
        gateway_id = gateway["Id"]
        report["safety_checks"] = {
            "docker_daemon": "reachable",
            "docker_context": "verified local Docker Desktop",
            "branch_and_commit": "recorded only",
            "compose_config": "isolated melt-s1 only",
            "runtime_network_internal": True,
            "no_env_file_or_variable_interpolation": True,
            "catalog_mongo_uri_local": True,
            "all_project_containers_on_s1_network": True,
            "exactly_two_catalog_replicas_healthy": True,
            "gateway_running": True,
        }
        write_report(report_path, report)

        docker("exec", "-i", gateway_id, "nginx", "-t", timeout=15)
        baseline_start = time.monotonic()
        for sequence in range(1, args.requests + 1):
            report["phases"]["baseline"].append(send_get(gateway_id, "baseline", sequence, run_id))
        attach_log_evidence(report["phases"]["baseline"], gateway_id, replica_summaries)
        baseline_instances = {item.get("replica_that_handled") for item in report["phases"]["baseline"]}
        if (
            any(item.get("http_status") != 200 or not item.get("gateway_log_found") or not item.get("catalog_log_found")
                for item in report["phases"]["baseline"])
            or len(baseline_instances) != 2
        ):
            raise SafetyError("Baseline did not return 200 from both replicas; refusing to stop a replica")
        report["phase_elapsed_seconds"] = {"baseline": round(time.monotonic() - baseline_start, 3)}
        write_report(report_path, report)

        # Re-read and validate Docker labels immediately before any stop operation.
        docker_context = verify_local_docker_context(expected_name=report["docker_context"]["name"])
        report["docker_context"] = docker_context
        fresh_replicas, _ = verify_project_shape(project_containers(), network_name)
        target = fresh_replicas[-1]
        target_id = target["Id"]
        target_labels = labels_of(inspect_container(target_id))
        if target_labels.get("com.docker.compose.project") != PROJECT or target_labels.get("com.docker.compose.service") != CATALOG:
            raise SafetyError("Target labels failed immediate pre-stop validation")
        healthy_others = [item for item in fresh_replicas if item["Id"] != target_id]
        if len(healthy_others) != 1 or not healthy_others[0].get("State", {}).get("Running") or healthy_others[0].get("State", {}).get("Health", {}).get("Status") != "healthy":
            raise SafetyError("The non-target replica is not healthy; refusing outage")
        report["target_replica"] = container_summary(target)
        report["healthy_replica_during_outage"] = container_summary(healthy_others[0])
        write_report(report_path, report)

        target_may_be_stopped = True
        docker("stop", "--time", "10", target_id, timeout=20)
        stopped = inspect_container(target_id)
        if stopped.get("State", {}).get("Running"):
            raise SafetyError("Docker stop returned but target is still running")
        target_stopped_monotonic = time.monotonic()
        report["target_stopped_at"] = now_local()

        current_replicas = [inspect_container(item["Id"]) for item in fresh_replicas if item["Id"] != target_id]
        if len(current_replicas) != 1 or not current_replicas[0].get("State", {}).get("Running") or current_replicas[0].get("State", {}).get("Health", {}).get("Status") != "healthy":
            raise SafetyError("The remaining Catálogo replica is not healthy after target stop")
        if not check_non_target_unchanged(initial_project_containers, target_id):
            raise SafetyError("A non-target melt-s1 container changed state during the outage")
        report["healthy_replica_during_outage"] = container_summary(current_replicas[0])
        outage_start = time.monotonic()
        for sequence in range(1, args.requests + 1):
            report["phases"]["one_replica_stopped"].append(send_get(gateway_id, "stopped", sequence, run_id))
        attach_log_evidence(report["phases"]["one_replica_stopped"], gateway_id, replica_summaries)
        report["phase_elapsed_seconds"]["one_replica_stopped"] = round(time.monotonic() - outage_start, 3)
        write_report(report_path, report)

    except BaseException as error:
        record_error("execution", error)
        if isinstance(error, (KeyboardInterrupt, SystemExit)):
            report["interrupted"] = True
    finally:
        if target_id and target_may_be_stopped:
            report["cleanup"]["attempted"] = True
            try:
                current = inspect_container(target_id)
                labels = labels_of(current)
                if labels.get("com.docker.compose.project") != PROJECT or labels.get("com.docker.compose.service") != CATALOG:
                    raise SafetyError("Cleanup refused: target labels no longer match melt-s1/catalogo-profesionales")
                if not current.get("State", {}).get("Running"):
                    target_restart_monotonic = time.monotonic()
                    report["recovery"]["restart_requested_at"] = now_local()
                    docker("start", target_id, timeout=20)
                restored, waited, last_state = wait_healthy(target_id, args.recovery_timeout)
                report["cleanup"].update({"restored": restored, "wait_seconds": round(waited, 3), "last_state": last_state})
                report["recovery"]["container_healthy_at"] = now_local() if restored else None
                if target_restart_monotonic is not None and restored:
                    report["recovery"]["container_health_seconds_from_restart"] = round(time.monotonic() - target_restart_monotonic, 3)
                if target_stopped_monotonic is not None and restored:
                    report["recovery"]["container_health_seconds_from_stop"] = round(time.monotonic() - target_stopped_monotonic, 3)
                if not restored:
                    raise RuntimeError(f"Target did not become healthy; last state: {last_state}")
            except BaseException as error:
                report["cleanup"]["error"] = f"{type(error).__name__}: {str(error)[:1000]}"
                record_error("cleanup", error)
        signal.signal(signal.SIGTERM, original_sigterm)

    if report["cleanup"].get("restored") and gateway_id and target_id:
        try:
            deadline = time.monotonic() + args.recovery_timeout
            recovery_sequence = 0
            target_seen_at: float | None = None
            # Continue until the peer is observed serving traffic and the normal
            # recovery sample is complete, or the guarded timeout expires.
            while time.monotonic() < deadline and (
                recovery_sequence < args.requests or target_seen_at is None
            ):
                recovery_sequence += 1
                item = send_get(gateway_id, "recovery", recovery_sequence, run_id)
                report["phases"]["recovery"].append(item)
                attach_log_evidence([item], gateway_id, replica_summaries)
                if item.get("replica_that_handled") == report["target_replica"].get("name") and item.get("http_status") == 200:
                    if target_seen_at is None:
                        target_seen_at = time.monotonic()
                        report["recovery"]["first_target_request_at"] = now_local()
                        if target_restart_monotonic is not None:
                            report["recovery"]["first_target_request_seconds_from_restart"] = round(target_seen_at - target_restart_monotonic, 3)
                if not item.get("gateway_log_found") or not item.get("catalog_log_found"):
                    time.sleep(0.2)
                    attach_log_evidence([item], gateway_id, replica_summaries)
                write_report(report_path, report)
                if target_seen_at is None:
                    time.sleep(0.5)
            report["recovery"]["target_seen_serving"] = target_seen_at is not None
            report["recovery"]["both_replicas_received_requests"] = {
                summary["name"]: any(req.get("replica_that_handled") == summary["name"] and req.get("http_status") == 200 for req in report["phases"]["recovery"])
                for summary in replica_summaries
            }
        except BaseException as error:
            record_error("recovery-probes", error)

    if initial_project_containers and target_id:
        try:
            report["cleanup"]["non_target_containers_unchanged"] = check_non_target_unchanged(initial_project_containers, target_id)
            if not report["cleanup"]["non_target_containers_unchanged"]:
                record_error("isolation-check", RuntimeError("A non-target melt-s1 container changed state"))
        except Exception as error:
            record_error("isolation-check", error)

    if replica_summaries:
        try:
            report["post_recovery_replica_health"] = verify_replicas_healthy_after_recovery(replica_summaries)
            if not all(report["post_recovery_replica_health"].values()):
                record_error("recovery-health", RuntimeError("Not all Catálogo replicas are healthy after recovery"))
        except Exception as error:
            record_error("recovery-health", error)

    # Keep unrelated local NGINX upstream DNS noise visible as warnings.
    if gateway_id:
        try:
            _, gateway_errors = gateway_events(gateway_id)
            request_ids = {
                request["request_id"]
                for phase in report["phases"].values()
                for request in phase
            }
            for line in gateway_errors[-100:]:
                if is_unrelated_upstream_warning(line, request_ids):
                    report["gateway_warnings"].append(line)
                else:
                    report["gateway_error_log_lines"].append(line)
                    report["errors"].append({"at": now_local(), "phase": "gateway-log", "message": line})
        except Exception as error:
            record_error("collect-gateway-errors", error)

    all_requests = [item for phase in report["phases"].values() for item in phase]
    unexpected = [
        f"{item['phase']} {item['request_id']}: HTTP {item.get('http_status')}"
        for item in all_requests if item.get("http_status") != 200
    ]
    missing_logs = [item["request_id"] for item in all_requests if not item.get("gateway_log_found") or not item.get("catalog_log_found")]
    report["errors"].extend({"at": now_local(), "phase": "http-response", "message": item} for item in unexpected)
    report["errors"].extend({"at": now_local(), "phase": "correlation", "message": f"Missing gateway/catalogue logs: {rid}"} for rid in missing_logs)
    phases_complete = (
        len(report["phases"]["baseline"]) == args.requests
        and len(report["phases"]["one_replica_stopped"]) == args.requests
        and len(report["phases"]["recovery"]) >= args.requests
    )
    if not phases_complete:
        report["errors"].append({"at": now_local(), "phase": "phase-completion", "message": "One or more request phases did not complete the expected sample"})
    all_http_200 = bool(all_requests) and all(item.get("http_status") == 200 for item in all_requests)
    correlation_complete = bool(all_requests) and all(
        item.get("gateway_log_found") and item.get("catalog_log_found") and item.get("replica_that_handled")
        for item in all_requests
    )
    fail_requests = report["phases"]["one_replica_stopped"]
    healthy_name = (report.get("healthy_replica_during_outage") or {}).get("name")
    continuity = len(fail_requests) == args.requests and all(
        item.get("http_status") == 200
        and item.get("replica_that_handled") == healthy_name
        and item.get("gateway_log_found")
        and item.get("catalog_log_found")
        for item in fail_requests
    )
    baseline_distribution = {item["name"]: 0 for item in replica_summaries}
    recovery_distribution = {item["name"]: 0 for item in replica_summaries}
    for item in report["phases"]["baseline"]:
        if item.get("replica_that_handled") in baseline_distribution and item.get("http_status") == 200:
            baseline_distribution[item["replica_that_handled"]] += 1
    for item in report["phases"]["recovery"]:
        if item.get("replica_that_handled") in recovery_distribution and item.get("http_status") == 200:
            recovery_distribution[item["replica_that_handled"]] += 1
    report["distributions"] = {
        "baseline": baseline_distribution,
        "one_replica_stopped": ({healthy_name: sum(1 for item in fail_requests if item.get("replica_that_handled") == healthy_name and item.get("http_status") == 200)} if healthy_name else {}),
        "recovery": recovery_distribution,
    }
    report["continuity_passed"] = continuity
    recovery_distribution_correct = (
        len(report["phases"]["recovery"]) >= args.requests
        and balanced_distribution(recovery_distribution)
    )
    replicas_healthy = (
        len(report["post_recovery_replica_health"]) == len(replica_summaries) == 2
        and all(report["post_recovery_replica_health"].values())
    )
    report["recovery_passed"] = bool(
        report["cleanup"].get("restored")
        and replicas_healthy
        and recovery_distribution_correct
        and all(item.get("http_status") == 200 for item in report["phases"]["recovery"])
        and all(item.get("gateway_log_found") and item.get("catalog_log_found") for item in report["phases"]["recovery"])
        and all(report["recovery"].get("both_replicas_received_requests", {}).values())
        and report["cleanup"].get("non_target_containers_unchanged") is True
    )
    baseline_balanced = (
        len(report["phases"]["baseline"]) == args.requests
        and all(item.get("http_status") == 200 for item in report["phases"]["baseline"])
        and all(item.get("gateway_log_found") and item.get("catalog_log_found") for item in report["phases"]["baseline"])
        and balanced_distribution(baseline_distribution)
    )
    report["checks"] = {
        "three_phases_complete": phases_complete,
        "all_responses_http_200": all_http_200,
        "baseline_distribution_correct": baseline_balanced,
        "outage_continuity": report["continuity_passed"],
        "recovery_distribution_correct": recovery_distribution_correct,
        "request_log_correlation_complete": correlation_complete,
        "replica_restored": report["cleanup"].get("restored") is True,
        "both_replicas_healthy_after_recovery": replicas_healthy,
        "no_relevant_errors": not report["errors"],
    }
    report["overall_passed"] = (
        all(report["checks"].values())
        and report["recovery_passed"]
    )
    report["finished_at"] = now_local()
    report["conclusion"] = (
        "PASS: balanced before outage, all outage reads served by the healthy replica, and both replicas served reads after recovery."
        if report["overall_passed"] else
        "FAIL/PARTIAL: inspect phase HTTP codes, upstreams, correlation logs, recovery state, and gateway error lines. No automatic configuration changes were made."
    )
    write_report(report_path, report)
    print(json.dumps({
        "report": str(report_path),
        "commit": report.get("commit"),
        "target_replica": (report.get("target_replica") or {}).get("name"),
        "baseline_distribution": report.get("distributions", {}).get("baseline"),
        "outage_distribution": report.get("distributions", {}).get("one_replica_stopped"),
        "recovery_distribution": report.get("distributions", {}).get("recovery"),
        "request_counts": {phase: len(items) for phase, items in report.get("phases", {}).items()},
        "continuity_passed": report.get("continuity_passed"),
        "recovery_passed": report.get("recovery_passed"),
        "overall_passed": report.get("overall_passed"),
        "cleanup": report.get("cleanup"),
        "errors": report.get("errors"),
    }, ensure_ascii=False, indent=2))
    return 0 if report["overall_passed"] else 1


if __name__ == "__main__":
    sys.exit(main())
