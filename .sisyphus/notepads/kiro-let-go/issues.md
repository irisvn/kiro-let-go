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
