## 2026-05-20 F3 manual QA

- QA workspace: `/tmp/kiro-qa-l9rIB2`
- Build command `go build -buildvcs=false ./cmd/server && go build -buildvcs=false ./cmd/cli` succeeded and produced runnable `./server` and `./cli` binaries.
- Server started successfully with temp config on `127.0.0.1:8765` and `/health` returned `200 {"status":"ok","version":"dev"}`.
- CLI add passed: `./cli --config /tmp/kiro-qa-l9rIB2/config.json --json account add --type apikey --label qa-cli --api-key ksk_test_cli --region us-east-1` created account `b137640b-7dee-4ca1-8adb-2350d107df7e`; SQLite row verified (`label=qa-cli`, `auth_method=apikey`, `api_key=ksk_test_cli`, `enabled=1`).
- REST add failed: `POST /admin/accounts` returned `404 page not found`; no `qa-rest` row was written to SQLite.
- File watcher sync passed: editing `/tmp/kiro-qa-l9rIB2/credentials.json` added `qa-file` and SQLite reflected the new row; later edit with `"enabled": false` updated SQLite to `enabled=0`.
- Proxy endpoints failed before upstream/auth behavior could be exercised:
  - `POST /v1/messages` returned `404 page not found` for both stream and non-stream requests.
  - `POST /v1/chat/completions` returned `404 page not found` for both stream and non-stream requests.
- Admin quota endpoint failed: `GET /admin/quota` returned `404 page not found` instead of JSON.
- Disable behavior only partially verified: CLI `account disable b137640b-7dee-4ca1-8adb-2350d107df7e --reason qa-disabled` updated SQLite (`enabled=0`, `disabled_reason=qa-disabled`), but route-level skip behavior could not be exercised because the proxy endpoints above returned 404.
- Graceful shutdown passed: sending `SIGTERM` to the server process shut it down in about `0.100s`; server log recorded `shutdown signal received` and `shutting down server`.
- Environment note: `sqlite3` CLI was not installed in this environment, so SQLite verification used Python's built-in `sqlite3` module against the generated DB file.
