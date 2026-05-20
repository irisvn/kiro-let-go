
## 2026-05-20 F1 rerun audit
- Read plan, README, configs, cmd/, and internal/ implementation files; re-ran guardrail searches and build/vet/test checks.
- `go vet ./...` passed. `go test ./...` passed. `go build ./cmd/server && go build ./cmd/cli` failed in this workspace because Go attempted VCS stamping; `go build -buildvcs=false -o bin/kiro-let-go ./cmd/server && go build -buildvcs=false -o bin/kiro-let-go-cli ./cmd/cli` passed.
- Verified smoke checks: `/health` returned `{"status":"ok","version":"dev"}`, `/v1/models` returned 200 with 6 models, `/admin/quota` returned 200, CLI `account add --type=apikey --key=ksk_test_dummy` wrote a SQLite row, and SIGTERM exited 0.
- Violations found:
  1. `internal/api/anthropic/handler.go:50-52` only registers `POST /v1/messages`; `POST /v1/messages/count_tokens` is missing and returned HTTP 404 in smoke testing.
  2. `internal/account/watcher.go:218-229` deletes every DB account missing from the file when `_remove_unlisted` is set; plan requires removing only file-managed accounts and preserving CLI/REST-managed accounts.
  3. `internal/cli/server.go:9-17` implements the `server` command as a message-only placeholder instead of invoking the server entrypoint with matching exit behavior.
  4. `internal/cli/account.go:289-294` plus `internal/cli/root.go:127-135` leave CLI `account refresh` without a social auth provider; social-account refresh will fail with `social auth refresher is nil`.
  5. `internal/server/middleware/auth.go:10-37` returns 401 JSON but does not emit a `WWW-Authenticate` / Bearer realm header required by the T30 auth acceptance criteria.
  6. `internal/account/circuit.go:157-170` and `internal/account/manager.go:158-159,276` implement deterministic every-Nth retries rather than probabilistic retry behavior for cooled-down accounts.
  7. `README.md:44-48` still documents `--api-key` instead of the implemented `--key` flag for CLI account creation.
