
## F2 Code Quality Review - 2026-05-20
- `go vet ./...`: PASS.
- `gofmt -l .`: FAIL. Unformatted files: `internal/account/circuit_test.go`, `internal/api/admin/handler.go`, `internal/api/admin/handler_test.go`, `internal/api/anthropic/types.go`, `internal/api/anthropic/types_test.go`, `internal/config/config.go`, `internal/converter/normalized_test.go`, `internal/kiro/dispatcher.go`.
- `staticcheck ./...`: PASS when rerun with `GOFLAGS=-buildvcs=false` (initial run failed only because workspace lacks VCS metadata).
- Exported API docs: FAIL. Automated scan found 172 exported identifiers in `internal/` without preceding doc comments, including `internal/account/manager.go:19`, `internal/account/circuit.go:11`, `internal/kiro/dispatcher.go:27`, `internal/kiro/types.go:14`, `internal/server/server.go:18`, `internal/normalized/normalized.go:5`, `internal/api/admin/handler.go:44`, `internal/api/anthropic/handler.go:34`, and `internal/api/openai/types.go:9`.
- TODO/FIXME audit: PASS. No `TODO`/`FIXME` markers found under `internal/`.
- Panic audit: FAIL. `internal/api/anthropic/types.go:592` calls `panic(err)` inside `mustMarshal`, which is reachable from JSON marshaling paths used by streamed/JSON responses (`internal/api/anthropic/types.go:377`, `434`, `544`).
- Silent error handling audit: FAIL.
  - `internal/api/openai/sse.go:171-173` discards `Write` errors while emitting SSE chunks.
  - `internal/kiro/dispatcher.go:106` discards `d.manager.Refresh(...)` errors on auth failures.
  - `internal/account/manager.go:205` and `217` discard `RecordSuccess` / `RecordFailure` errors during request completion.
- Concurrency audit: PASS.
  - Token refresh uses per-account locking plus a second freshness check after reload (`internal/account/manager.go:223-235`), i.e. double-checked locking.
  - Account manager uses `sync.RWMutex` consistently for sticky-session state (`internal/account/manager.go:173-175`, `211-213`).

## F2 Follow-up Fixes - 2026-05-20
- `internal/api/anthropic/types.go`: `mustMarshal` now returns errors, and Anthropic JSON/SSE marshaling paths propagate them instead of panicking.
- `internal/api/anthropic/handler.go`: non-streaming responses are pre-marshaled so marshal failures return a 500 error instead of partially committing a 200 response.
- `internal/api/openai/sse.go`: SSE writes now warn on unexpected write/flush failures, while suppressing normal client-disconnect errors such as `io.ErrClosedPipe` and `context.Canceled`.

## F1/F2 verification-wave fixes - 2026-05-20
- Silent completion-path store errors in `internal/account/manager.go` now emit WARN logs with `account_id`, and failure logging also includes `reason`.
- Store write operations now use `&sql.TxOptions{Isolation: sql.LevelSerializable}` consistently for SQLite `BEGIN IMMEDIATE` semantics.
- Verification passed with `go build -buildvcs=false ./cmd/server && go build -buildvcs=false ./cmd/cli` and `go test -buildvcs=false ./...`.
