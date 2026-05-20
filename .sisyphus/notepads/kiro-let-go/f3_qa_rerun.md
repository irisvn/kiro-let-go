## 2026-05-20 F3 manual QA rerun

- QA workspace: `/tmp/opencode/kiro-qa-rerun-2_3difn2`
- Build command `go build -buildvcs=false ./cmd/server && go build -buildvcs=false ./cmd/cli` succeeded.
- Started `./kiro-let-go server --config /tmp/opencode/kiro-qa-rerun-2_3difn2/config.json` (symlinked to built `./server`), `/health` returned `200 {"status":"ok","version":"dev"}`.
- CLI add passed: `./kiro-let-go-cli --config ... --json account add --type apikey --label qa-cli-rerun --key ksk_test_cli_rerun --region us-east-1` created account `63ffbafb-ab05-49a3-97c0-55c997443129`; SQLite row verified with `api_key=ksk_test_cli_rerun`, `enabled=1`.
- REST add passed: `POST /admin/accounts` returned `201`; SQLite row verified for `qa-rest-rerun` with `api_key=ksk_test_rest_rerun`, `enabled=1`.
- File watcher sync passed: editing `/tmp/opencode/kiro-qa-rerun-2_3difn2/credentials.json` added `qa-file-rerun`; SQLite row verified with `api_key=ksk_test_file_rerun`, `enabled=1`.
- Anthropic endpoint no longer 404s: `POST /v1/messages` returned `503` with JSON error `NO_ACCOUNTS_AVAILABLE`. Server log shows all three dummy accounts were attempted and marked `auth_expired`.
- OpenAI endpoint no longer 404s: `POST /v1/chat/completions` returned `500` JSON `internal_error/no available candidates` after the prior Anthropic request opened circuits on all dummy accounts.
- Admin quota endpoint passed: `GET /admin/quota` returned `200` with a JSON array. Observed item keys: `account_id`, `label`, `subscription_title`, `limit_total`, `limit_remaining`, `fetched_at`, `stale`.
- Disable behavior passed, with targeted follow-up verification:
  - `./kiro-let-go-cli --config ... account disable 63ffbafb-ab05-49a3-97c0-55c997443129 --reason qa-disabled-rerun` updated SQLite to `enabled=0`, `disabled_reason=qa-disabled-rerun`.
  - Because the first proxy call had already opened circuits on the other enabled dummy accounts, a second server run added fresh enabled account `qa-skip-fresh` via REST.
  - Follow-up `POST /v1/messages` returned `503`, but disabled account `qa-cli-rerun` stayed at `failure_count=1` while fresh enabled account `qa-skip-fresh` changed `failure_count 0 -> 1`, confirming the disabled account was skipped.
- SIGTERM graceful shutdown passed: server exited cleanly in about `0.031s` (< 30s).
- Overall result: `APPROVED`.
