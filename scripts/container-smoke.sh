#!/usr/bin/env bash
set -euo pipefail
image_ref=${1:?usage: container-smoke.sh IMAGE}
smoke_dir=$(mktemp -d)
smoke_name="notebook-smoke-$(basename "$smoke_dir" | tr -cd '[:alnum:]')"
cleanup() {
  docker rm -f "$smoke_name" >/dev/null 2>&1 || true
  # Linux bind mounts retain the image user's ownership on directories it made.
  rm -rf "$smoke_dir" 2>/dev/null || sudo rm -rf -- "$smoke_dir"
}
trap cleanup EXIT
[[ $(docker image inspect "$image_ref" --format '{{.Config.User}}') == 65532:65532 ]]
mkdir -p "$smoke_dir/vault/private"
chmod 755 "$smoke_dir"
chmod 777 "$smoke_dir/vault" "$smoke_dir/vault/private"
python3 - "$smoke_dir/vault" <<'PY'
import pathlib, sys
root = pathlib.Path(sys.argv[1])
(root / 'Welcome.md').write_bytes(b'\xef\xbb\xbf---\r\ntags: [demo]\r\n---\r\n# Welcome\r\n\r\nNotebook synthetic fixture.\r\n')
(root / 'private/Hidden.md').write_text('ExcludedSmokeSentinel\n')
for note in root.rglob('*.md'):
    note.chmod(0o666)
PY
start_server() {
  docker run -d --name "$smoke_name" --read-only --tmpfs /tmp \
    -p 127.0.0.1::8080 -v "$smoke_dir/vault:/vaults/notes" \
    -e NOTEBOOK_VAULTS=notes=/vaults/notes \
    -e NOTEBOOK_MCP_EXCLUDE=notes:private "$image_ref" >/dev/null
  smoke_base="http://$(docker port "$smoke_name" 8080/tcp)"
  for _ in {1..100}; do
    if curl --fail --silent "$smoke_base/readyz" >/dev/null; then return; fi
    sleep .2
  done
  docker logs "$smoke_name"
  return 1
}
request() {
  curl --fail --silent --show-error -H 'Content-Type: application/json' -H 'X-Notebook: 1' "$@"
}
start_server
curl --fail --silent "$smoke_base/healthz" >/dev/null
curl --fail --silent "$smoke_base/" > "$smoke_dir/index.html"
python3 - "$smoke_dir/index.html" > "$smoke_dir/asset" <<'PY'
import re, sys
html = open(sys.argv[1]).read()
assert '<title>Notebook</title>' in html
asset = re.search(r'src="(/assets/[^\"]+\.js)"', html)
assert asset, 'embedded frontend asset missing'
print(asset.group(1))
PY
curl --fail --silent "$smoke_base$(cat "$smoke_dir/asset")" >/dev/null
curl --fail --silent "$smoke_base/api/docs" >/dev/null
curl --fail --silent "$smoke_base/api/openapi.json" > "$smoke_dir/schema.json"
python3 - "$smoke_dir/schema.json" <<'PY'
import json, sys
assert '/api/vaults/{vault}/note' in json.load(open(sys.argv[1]))['paths']
PY
# Saving untouched BOM/CRLF content must preserve every byte.
curl --fail --silent "$smoke_base/api/vaults/notes/note?path=Welcome.md" > "$smoke_dir/loaded.json"
python3 - "$smoke_dir" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
note = json.loads((root / 'loaded.json').read_text())
assert note['content'].encode() == (root / 'vault/Welcome.md').read_bytes()
(root / 'save.json').write_text(json.dumps({k: note[k] for k in ('content', 'revision')}))
(root / 'expected.bin').write_bytes((root / 'vault/Welcome.md').read_bytes())
PY
request -X PUT --data-binary "@$smoke_dir/save.json" "$smoke_base/api/vaults/notes/note?path=Welcome.md" >/dev/null
cmp "$smoke_dir/expected.bin" "$smoke_dir/vault/Welcome.md"
request -d '{"path":"Smoke.md","content":"Created smoke note\n"}' "$smoke_base/api/vaults/notes/notes" >/dev/null
curl --fail --silent "$smoke_base/api/vaults/notes/note?path=Smoke.md" > "$smoke_dir/created.json"
python3 - "$smoke_dir" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
note = json.loads((root / 'created.json').read_text())
assert note['content'] == 'Created smoke note\n'
assert (root / 'vault/Smoke.md').read_bytes() == b'Created smoke note\n'
(root / 'update.json').write_text(json.dumps({'content': 'Updated smoke note\n', 'revision': note['revision']}))
PY
request -X PUT --data-binary "@$smoke_dir/update.json" "$smoke_base/api/vaults/notes/note?path=Smoke.md" > "$smoke_dir/updated.json"
# A stale revision must not overwrite the update.
[[ $(curl --silent -o /dev/null -w '%{http_code}' -X PUT -H 'Content-Type: application/json' -H 'X-Notebook: 1' --data-binary "@$smoke_dir/update.json" "$smoke_base/api/vaults/notes/note?path=Smoke.md") == 409 ]]
go run ./testdata/compat/mcp_smoke.go -query 'Notebook synthetic fixture' -absent ExcludedSmokeSentinel "$smoke_base/mcp"
docker rm -f "$smoke_name" >/dev/null
start_server
curl --fail --silent "$smoke_base/api/vaults/notes/note?path=Smoke.md" > "$smoke_dir/restarted.json"
python3 - "$smoke_dir" <<'PY'
import json, pathlib, sys
root = pathlib.Path(sys.argv[1])
assert json.loads((root / 'updated.json').read_text()) == json.loads((root / 'restarted.json').read_text())
assert (root / 'vault/Smoke.md').read_bytes() == b'Updated smoke note\n'
PY
request -X DELETE "$smoke_base/api/vaults/notes/entry?path=Smoke.md" >/dev/null
[[ ! -e "$smoke_dir/vault/Smoke.md" ]]
python3 - "$smoke_dir/vault" <<'PY'
import pathlib, sys
trashed = list((pathlib.Path(sys.argv[1]) / '.trash').glob('*-Smoke.md'))
assert len(trashed) == 1
assert trashed[0].read_bytes() == b'Updated smoke note\n'
PY
printf 'Container verified: embedded UI, API, nonroot storage, exact bytes, revision conflicts, MCP exclusions, restart persistence and trash.\n'
