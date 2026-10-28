"""D8 read-only checks against OpenAPI, real gateway, replicas and MongoDB."""

import argparse
import datetime
import hashlib
import importlib.util
import json
from pathlib import Path
import sys
import urllib.parse
import uuid

import yaml
from openapi_spec_validator import validate_spec
from openapi_schema_validator import OAS30Validator

ROOT = Path(__file__).resolve().parents[1]
sys.dont_write_bytecode = True


def module(name, filename):
    spec = importlib.util.spec_from_file_location(name, ROOT / "scripts" / filename)
    result = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(result)
    return result


d11 = module("d11", "verify-observability.py")
d3 = module("d3", "verify-persistence.py")


def expand(value, contract):
    if isinstance(value, dict):
        if "$ref" in value:
            target = contract
            for part in value["$ref"].split("/")[1:]:
                target = target[part]
            return expand(target, contract)
        return {k: expand(v, contract) for k, v in value.items()}
    if isinstance(value, list):
        return [expand(v, contract) for v in value]
    return value


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--gateway", default="http://localhost:8080")
    parser.add_argument("--jaeger", default="http://localhost:16686")
    parser.add_argument("--output", type=Path)
    args = parser.parse_args()
    contract_path = ROOT / "docs/contracts/tratamientos-v1.openapi.yaml"
    contract = yaml.safe_load(contract_path.read_text(encoding="utf-8"))
    validate_spec(contract)
    before = d3.snapshot()
    documents = before["documents"]
    existing = next((d for d in documents if d.get("id") == "test-001"), None)
    d11.check(existing is not None, "Existing test-001 required; no data will be created")
    treatment_validator = OAS30Validator(expand(contract["components"]["schemas"]["Tratamiento"], contract))
    wrong_id = dict(existing, id=1)
    missing_keywords = {k: v for k, v in existing.items() if k != "keywords"}
    d11.check(not treatment_validator.is_valid(wrong_id), "Contract did not detect numeric ID")
    d11.check(not treatment_validator.is_valid(missing_keywords), "Contract did not detect missing keywords")
    evidence = {
        "captured_at_utc": datetime.datetime.now(datetime.timezone.utc).isoformat(),
        "branch": d3.command("git", "branch", "--show-current"),
        "base_commit": d3.command("git", "rev-parse", "HEAD"),
        "read_only": True, "contract_version": contract["info"]["version"],
        "contract_sha256": hashlib.sha256(contract_path.read_bytes()).hexdigest(),
        "openapi_validation": "passed", "schema_detects_numeric_id_and_missing_keywords": True,
        "requests": [],
    }
    cases = [("default", {}, None)]
    cases += [("search_name", {"q": existing["nombre"]}, None),
              ("search_keyword", {"q": existing["keywords"][-1]}, None),
              ("no_results", {"q": "d8-absent-" + uuid.uuid4().hex}, None)]
    categories = contract["components"]["schemas"]["Tratamiento"]["properties"]["categoria"]["enum"]
    cases += [("category_" + str(i), {"categoria": c}, None) for i, c in enumerate(categories)]
    cases += [("price_inclusive", {"precioMax": existing["precio"]}, None),
              ("price_exclusive", {"precioMax": max(0, existing["precio"] - 1)}, None),
              ("page_one", {"page": 1, "limit": 1}, None),
              ("page_two", {"page": 2, "limit": 1}, None),
              ("limit_max", {"limit": 50}, None),
              ("combined", {"q": existing["keywords"][-1], "categoria": existing["categoria"],
                            "precioMax": existing["precio"], "page": 1, "limit": 10,
                            "sort": "precio", "order": "desc"}, None)]
    cases += [("sort_" + field + "_" + order, {"sort": field, "order": order}, None)
              for field in ["nombre", "precio"] for order in ["asc", "desc"]]
    cases += [("invalid_" + str(i), query, "INVALID_PARAMETER") for i, query in enumerate([
        {"page": 0}, {"page": "abc"}, {"limit": 0}, {"limit": 51}, {"sort": "invalid"},
        {"order": "invalid"}, {"precioMax": -1}, {"precioMax": "abc"}])]
    cases += [("detail", None, None), ("missing", None, "TRATAMIENTO_NOT_FOUND")]
    trace_for_list = None
    for label, query, error_code in cases:
        detail = query is None
        suffix = "/test-001" if label == "detail" else "/d8-absent-" + uuid.uuid4().hex if label == "missing" else ""
        url_query = "?" + urllib.parse.urlencode(query) if query else ""
        path = "/api/v1/tratamientos" + suffix + url_query
        rid = "sofia-d8-" + uuid.uuid4().hex
        tid = uuid.uuid4().hex
        parent = uuid.uuid4().hex[:16]
        status, headers, body = d11.get_json(args.gateway.rstrip("/") + path,
                                            {"X-Request-ID": rid, "traceparent": "00-" + tid + "-" + parent + "-01"})
        expected_status = 404 if label == "missing" else 400 if error_code else 200
        d11.check(status == expected_status, label + ": wrong status")
        operation = contract["paths"]["/tratamientos/{id}" if detail else "/tratamientos"]["get"]
        schema = operation["responses"][str(status)]["content"]["application/json"]["schema"]
        OAS30Validator(expand(schema, contract), format_checker=OAS30Validator.FORMAT_CHECKER).validate(body)
        d11.check("application/json" in d11.response_header(headers, "Content-Type"), "Response is not JSON")
        d11.check(d11.response_header(headers, "X-Request-ID") == rid, "Request ID lost")
        d11.check(d11.response_header(headers, "X-Trace-ID") == tid, "Trace ID lost")
        if error_code:
            d11.check(body["error"]["code"] == error_code, "Wrong error code")
        elif detail:
            d11.check(body == existing, "Detail differs from MongoDB")
        else:
            selected = documents[:]
            if query.get("q"):
                q = query["q"].casefold()
                selected = [d for d in selected if q in d["nombre"].casefold() or any(q in k.casefold() for k in d["keywords"])]
            if query.get("categoria"):
                selected = [d for d in selected if d["categoria"].casefold() == query["categoria"].casefold()]
            if "precioMax" in query:
                selected = [d for d in selected if d["precio"] <= query["precioMax"]]
            selected.sort(key=lambda d: d["id"])
            selected.sort(key=lambda d: d[query.get("sort", "nombre")], reverse=query.get("order") == "desc")
            page, limit = query.get("page", 1), query.get("limit", 10)
            expected = {"tratamientos": selected[(page - 1) * limit:page * limit],
                        "pagination": {"page": page, "limit": limit, "total": len(selected),
                                       "totalPages": (len(selected) + limit - 1) // limit}}
            d11.check(body == expected, label + ": filters/pagination differ from real data")
        old_status, _, old_body = d11.get_json(args.gateway.rstrip("/") + "/api/v1/catalogo/tratamientos" + suffix + url_query)
        d11.check(old_status == status and old_body == body, "Legacy route compatibility broken")
        evidence["requests"].append({"case": label, "status": status, "schema_valid": True,
                                     "query": query, "legacy_matches": True, "request_id": rid, "trace_id": tid})
        if label == "default":
            trace_for_list = (tid, parent)
        print("PASS", label, status)

    trace = d11.wait_for_trace(args.jaeger.rstrip("/"), trace_for_list[0])
    gateway_span = d11.verify_trace(trace)
    d11.check(d11.parent(gateway_span) == trace_for_list[1], "Remote trace parent lost")
    records = d11.read_logs()
    for request in evidence["requests"]:
        logs = [r for r in records if r.get("request_id") == request["request_id"] and "method" in r]
        d11.check({r.get("service") for r in logs} >= {"melt-gateway", "catalogo-profesionales"}, "Correlated logs missing")
        d11.check(all(r.get("trace_id") == request["trace_id"] and r.get("status") == request["status"] for r in logs), "Log mismatch")
        request["correlated_logs"] = logs
    evidence["trace"] = {"id": trace_for_list[0], "parent_propagated": True,
                          "spans": [{"service": trace["processes"][s["processID"]]["serviceName"],
                                     "operation": s["operationName"], "span_id": s["spanID"],
                                     "parent_span_id": d11.parent(s)} for s in trace["spans"]]}
    evidence["replicas"] = []
    for cid in d3.command("docker", "compose", "ps", "-q", "catalogo-profesionales").splitlines():
        name = d3.command("docker", "inspect", "--format", "{{.Name}}", cid).lstrip("/")
        actual = json.loads(d11.docker("compose", "exec", "-T", "gateway", "wget", "-qO-", "-T", "5",
                                      "http://" + name + ":8081/tratamientos/test-001"))
        d11.check(actual == existing, "Replica differs from real MongoDB data")
        evidence["replicas"].append({"name": name, "detail_matches_mongodb": True})
    d11.check(len(evidence["replicas"]) == 2, "Expected two replicas")
    d11.check(d3.snapshot() == before, "Data/index snapshot changed; inspect concurrent writes")
    evidence["mongodb"] = {"count": len(documents), "snapshot_unchanged": True,
                            "document_sha256": d3.digest(existing)}
    evidence["edge_input_observations"] = []
    for query in [{"categoria": "Inexistente"}, {"precioMax": "NaN"}, {"precioMax": "Inf"}, {"page": 1000001}]:
        status, _, body = d11.get_json(args.gateway.rstrip("/") + "/api/v1/tratamientos?" + urllib.parse.urlencode(query))
        evidence["edge_input_observations"].append({"query": query, "observed_status": status,
                                                    "total": body.get("pagination", {}).get("total"),
                                                    "error_code": body.get("error", {}).get("code"),
                                                    "contract_conformance_asserted": False})
    evidence["limitations"] = ["Single stored document: ordering across multiple records not demonstrated.",
                               "No dependency failure or Internet publication tested.",
                               "Mock divergences and edge-input limitations documented separately."]
    if args.output:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(evidence, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    print("PASS", len(evidence["requests"]), "contract cases, legacy compatibility, traces, logs, two replicas and MongoDB")


if __name__ == "__main__":
    main()
