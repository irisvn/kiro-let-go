## T24 Kiro stream decoder
- Full package test run currently fails in unrelated `client_test.go` expectations (`Received unexpected error: <nil>` in proxy/retry/stream body tests).

## T21 Kiro HTTP client
- The prior T24 note about `client_test.go` failures is resolved by the T21 client implementation; `go test ./...` now passes.

## T28 JSON file watcher - 2026-05-20
- `go test ./...` is currently blocked by pre-existing failures: undefined account.NoAccountsAvailable in internal/kiro/dispatcher.go and circuit breaker test expectation mismatches.

## T25 Account manager - 2026-05-20
- Account package race tests now pass after making circuit breaker probabilistic retry deterministic and fixing the watcher/account↔kiro test import cycle.

## T36 Admin REST API - 2026-05-20
- `go test ./...` is still blocked by pre-existing failures outside T36: an `internal/api/openai` ↔ `internal/converter` import cycle and unresolved `internal/api/anthropic` handler compile errors (`anthropicToNormalized` / `normalizedToKiro` undefined, stale unused imports).

## F4 Scope Fidelity Audit - 2026-05-20
- REJECT: Chat request validation accepts arbitrary model IDs. `internal/kiro/types.go:34-35` returns unknown model names unchanged, while `internal/api/openai/types.go:172-193` and `internal/api/anthropic/types.go:26-42` only require a non-empty model. This allows GPT/Gemini/non-Claude requests despite the agreed Claude-only scope.

## F2/F4 Remediation - 2026-05-20
- No unresolved blockers after remediation: `go build -buildvcs=false ./...`, `go test -buildvcs=false ./...`, and `go vet ./...` pass.
