import argparse
import copy
import json
import pathlib
import subprocess
import tempfile

ROOT = pathlib.Path(__file__).resolve().parents[1]
parser = argparse.ArgumentParser(description='Deterministic 3.1 -> 3.0 projection for pinned oapi-codegen; never edits source.')
parser.add_argument('--check', action='store_true')
args = parser.parse_args()
spec = json.loads((ROOT.parent / 'contracts/openapi.json').read_text())
assert spec['openapi'] == '3.1.0'

def project(node):
    if isinstance(node, list):
        return [project(x) for x in node]
    if not isinstance(node, dict):
        return node
    node = copy.deepcopy(node)
    if 'anyOf' in node and {'type': 'null'} in node['anyOf']:
        alternatives = [x for x in node.pop('anyOf') if x != {'type': 'null'}]
        assert len(alternatives) == 1, 'Unsupported nullable union'
        node.update(alternatives[0])
        node['nullable'] = True
        if '$ref' in node:
            node['allOf'] = [{'$ref': node.pop('$ref')}]
    if 'const' in node:
        node['enum'] = [node.pop('const')]
    return {k: project(v) for k, v in node.items()}

spec = project(spec)
spec['openapi'] = '3.0.3'
with tempfile.TemporaryDirectory(prefix='remote-agent-session-openapi-') as directory:
    source = pathlib.Path(directory) / 'openapi.json'
    source.write_text(json.dumps(spec))
    subprocess.run(['go', 'run', 'github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.5.1',
                    '-generate', 'types,client', '-package', 'api', '-o',
                    str(pathlib.Path(directory) / 'generated.go'), str(source)], check=True)
    subprocess.run(['go', 'run', str(ROOT / 'tools/normalize-client.go'), '--',
                    str(pathlib.Path(directory) / 'generated.go')], check=True)
    generated = (pathlib.Path(directory) / 'generated.go').read_bytes()
    target = ROOT / 'internal/api/generated.go'
    if args.check:
        if not target.exists() or target.read_bytes() != generated:
            raise SystemExit('Generated Go client differs; run python3 tools/generate.py')
    else:
        target.write_bytes(generated)
