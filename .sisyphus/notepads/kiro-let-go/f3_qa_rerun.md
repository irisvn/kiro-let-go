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

## 2026-05-20 F3 manual QA rerun (clean DB isolation)

- Re-ran F3 with isolated SQLite databases per scenario group under `/tmp/kiro-qa2`, specifically to eliminate the two prior false failures.
- `make build` succeeded and produced fresh `bin/kiro-let-go` + `bin/kiro-let-go-cli`.
- Scenario 1 passed on `127.0.0.1:18766`: `/health` returned `200` with JSON `{"status":"ok","version":"61e579a"}` and SIGTERM exit code was `0`.
- Scenario 2 passed on the configured port `18766`: `/health=200`, `POST /v1/messages` without auth `=401`, with wrong bearer key `=401`, `GET /admin/accounts` without auth `=401`, and with admin bearer key `=200`.
- Scenario 3 passed with a fresh DB: CLI `account add --json` succeeded and `account list --json` returned exactly one SQLite-backed account row.
- Scenario 4 passed with a fresh DB: `POST /admin/accounts` returned `201`, and `GET /admin/quota` returned `200` with a JSON array containing the added account.
- Scenario 5 passed with a fresh DB: `GET /v1/models` returned `200`, `{"object":"list"}`, and exactly 6 Claude model IDs.
- Scenario 6 passed with a truly clean DB (`clean.db`): CLI add/list/disable/enable/remove all succeeded, and final `account list --json` returned `[]`.
- Scenario 7 passed via Scenario 1 evidence: SIGTERM shutdown remained graceful with exit code `0`.
- Clean-isolation verdict stayed `APPROVED`; the earlier failures were confirmed to be QA/test-design issues rather than product defects.
