"""Validate catalog OpenAPI and actual HTTP responses exported by the Go integration test.

Usage: python scripts/validar-contrato-catalogo.py /path/to/responses.json
Dependencies: openapi-spec-validator (includes PyYAML and openapi-schema-validator).
"""
import json
from pathlib import Path
import sys

import yaml
from jsonschema import RefResolver
from openapi_schema_validator import OAS30Validator
from openapi_spec_validator import validate_spec

root = Path(__file__).resolve().parents[1] / 'docs' / 'contracts'
names = ('catalogo-admin-v1.openapi.json', 'tratamientos-v1.openapi.yaml')
contracts = {}
for name in names:
    document = yaml.safe_load((root / name).read_text(encoding='utf-8'))
    validate_spec(document)
    contracts[name] = document
    print(name + ': valid OpenAPI')

records = json.loads(Path(sys.argv[1]).read_text(encoding='utf-8'))
if len(records) < 7:
    raise ValueError('Expected real response fixtures from TestAuditContractFixtureExport')
for item in records:
    contract = contracts[item['contract']]
    schema = contract['components']['schemas'][item['schema']]
    OAS30Validator(schema, resolver=RefResolver.from_schema(contract)).validate(item['response'])
print(f'{len(records)} real handler responses match OpenAPI')
