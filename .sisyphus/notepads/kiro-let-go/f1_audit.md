
## 2026-05-20 F1 audit
- Ran `GOFLAGS=-buildvcs=false go build ./cmd/server && go build ./cmd/cli`, `go vet ./...`, `go test ./...`, and `make build`; all passed.
- Verified CLI can create a SQLite row with `./kiro-let-go-cli --config configs/config.example.json account add --type=apikey --api-key=ksk_test_dummy --label=t1`; `.data/kiro.db` contains the account row.
- Verified server health and SIGTERM lifecycle: `/health` returned 200 and shutdown exit code was 0.
- Found plan-compliance violations:
  1. `internal/server/server.go` still wires stub routes only; runtime `POST /v1/messages`, `POST /v1/chat/completions`, and `GET /admin/quota` returned 404 during smoke tests.
  2. `internal/server/server.go:52-62` serves stub `/v1/models` and `/admin/accounts` handlers instead of the real handlers from `internal/api/openai`, `internal/api/anthropic`, and `internal/api/admin`.
  3. `internal/api/anthropic/handler.go:75` and `internal/api/openai/handler.go:57` call `NormalizedToKiro(..., "")`, so account `profile_arn` is never forwarded into `KiroPayload.profileArn`.
  4. `internal/account/store.go` uses `BeginTx(ctx, nil)` for write paths and `RecordFailure(ctx, id string)` omits the required reason argument, diverging from T6.
  5. `internal/api/anthropic/types.go:592` contains `panic(err)` in production request-path code, violating the no-panic guardrail.
  6. CLI contract drift: `./kiro-let-go-cli account add --type=apikey --key=...` fails with `unknown flag: --key`; implementation expects `--api-key` (`internal/cli/account.go:80`). `internal/cli/server.go:9-17` prints a message instead of aliasing the server entrypoint.
  7. `cmd/cli/main.go:10-17` bypasses `cli.Execute()`, so reserved-name validation and the required `[error]` formatting are not applied.
- Guardrail checks: no TODO/FIXME matches; no production `mock`/`stub` matches under `internal/kiro`; no OIDC/SSO/Web UI/compression/WebSearch MCP code found.

## 2026-05-20 F1/F2 fix follow-up
- `internal/account/store.go` now takes `RecordFailure(ctx, id, reason)`, logs the failure reason via `slog.Warn`, and uses serializable write transactions so modernc/sqlite issues `BEGIN IMMEDIATE` on all write paths.
- `internal/account/manager.go` now warns instead of silently discarding `RecordSuccess` / `RecordFailure` store errors, preserving existing control flow.
- CLI contract fixes landed: `account add` now uses `--key`, CLI init now errors when the configured SQLite DB file is missing instead of creating directories/running migrations, and `cmd/cli/main.go` now routes through `cli.Execute()` with `[error]` stderr formatting.
