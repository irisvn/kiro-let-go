# Kiro AI Client (kiro-let-go) — Go Gateway Proxy

## TL;DR
> **Summary**: Build a Go-based gateway proxy that fronts multiple Kiro AI accounts, exposing OpenAI and Anthropic-compatible APIs with multi-account failover, anti-ban protections, and quota inspection.
> **Deliverables**:
> - Single `kiro-let-go` server binary (gin + SQLite + Kiro protocol client)
> - Single `kiro-let-go-cli` binary (cobra-based account management)
> - REST Admin API for account CRUD + quota inspection
> - JSON file watcher for declarative account sync
> - Anthropic `/v1/messages` and OpenAI `/v1/chat/completions` compatibility (streaming + non-streaming)
> - 5 anti-ban techniques: per-account machine ID, header randomization, per-account proxy, health-probe avoidance, failure-based cooldown
> - SQLite-backed persistence (modernc.org/sqlite, pure-Go)
> **Effort**: Large
> **Parallel**: YES — 7 implementation waves + 1 final verification wave
> **Critical Path**: T1 → T2/T4 → T6/T11 → T20/T24 → T25/T29 → T30 → T38 → F1-F4

## Context
### Original Request
Vietnamese-language request: build a Go-based "Kiro AI Client" gateway proxy that:
- Manages multiple Kiro AI accounts
- Compatible with OpenAI and Anthropic APIs
- Easy account addition
- Anti-ban protection
- Quota inspection

Reference repos provided: `kiro-account-manager` (Rust/Tauri), `kiro-gateway` (Python/FastAPI), `kiro.rs` (Rust).

### Interview Summary
User selected:
- HTTP framework: **gin**
- Storage: **SQLite** (modernc.org/sqlite, pure-Go, no CGO)
- Auth methods: **Social refreshToken + API Key** (no IdC/AWS SSO OIDC)
- Account UX: **CLI + REST Admin API + JSON file watch** (3 channels)
- Admin: **REST API only**, no UI
- Tests: **tests-after** (post-implementation, with agent QA always)
- Anti-ban: **5 techniques** — per-account machine ID, header randomization, per-account proxy, health probe avoidance, failure-based cooldown

### Self-Review (gaps addressed)
The following gaps were identified during self-review and resolved in the plan:
- **Concurrency model**: explicit per-account `sync.Mutex` for token refresh (double-checked locking pattern from kiro.rs); `sync.RWMutex` on account manager for selection
- **Graceful shutdown**: SIGTERM triggers `http.Server.Shutdown` with 30s deadline, drains in-flight streaming requests via `context.Context` cancellation
- **DB migrations**: applied on startup via embedded SQL files in `internal/account/migrations/`
- **Token refresh during streaming**: refresh BEFORE the upstream call only, never mid-stream; if upstream returns 401/403 mid-stream, abort and retry with new account
- **Client disconnect**: detect via request `context.Done()` and propagate cancellation to upstream HTTP client
- **Profile ARN**: stored per-account in DB, sent in request body
- **Default region**: `us-east-1`, configurable globally and overridable per-account
- **Default server port**: 8765 (matching kiro-account-manager convention)
- **Token refresh threshold**: 5 minutes before expiry
- **Quota cache TTL**: 12h (matches kiro-gateway `ACCOUNT_CACHE_TTL`)
- **Circuit breaker**: BASE 60s, max multiplier 1440x (matches kiro-gateway)

## Work Objectives
### Core Objective
Build a production-quality Go HTTP gateway that translates standard Anthropic/OpenAI API requests into Kiro AI's proprietary AWS-Event-Stream protocol, balances load across multiple accounts, recovers from failures, exposes quota information, and resists Kiro's ban-detection mechanisms.

### Deliverables
1. `cmd/server/main.go` — Gateway HTTP server (gin)
2. `cmd/cli/main.go` — Account management CLI (cobra)
3. `internal/api/anthropic` — Anthropic Messages API surface
4. `internal/api/openai` — OpenAI Chat Completions API surface
5. `internal/api/admin` — Admin REST API for account CRUD + quota
6. `internal/kiro` — Kiro protocol client (auth, headers, AWS Event Stream parser)
7. `internal/converter` — Request/response conversion (Anthropic↔Normalized↔Kiro, OpenAI↔Normalized↔Kiro)
8. `internal/account` — SQLite store, account manager, balancer, circuit breaker, file watcher
9. `internal/antiban` — Header randomization helpers, proxy resolver, health-probe TTL
10. `internal/thinking` — FSM-based `<thinking>` tag parser
11. `internal/streaming` — SSE writer helpers
12. `configs/config.example.json`, `configs/credentials.example.json`
13. `README.md` with operational guide

### Definition of Done
Verifiable conditions (commands an agent can run):
- [ ] `go build ./cmd/server && go build ./cmd/cli` exits 0
- [ ] `go vet ./...` exits 0
- [ ] `go test ./...` exits 0 (tests added in tests-after pass)
- [ ] `./kiro-let-go-cli account add --type=apikey --key=ksk_test_dummy` writes a row to SQLite (verifiable via `sqlite3 .data/kiro.db "SELECT * FROM accounts"`)
- [ ] `./kiro-let-go server` listens on `:8765` and `curl http://127.0.0.1:8765/health` returns `{"status":"ok"}`
- [ ] `curl -X POST http://127.0.0.1:8765/v1/messages -H "x-api-key: $KEY" -H "anthropic-version: 2023-06-01" -d @sample-anthropic.json` returns a real Anthropic SSE stream (when given a valid Kiro account)
- [ ] `curl -X POST http://127.0.0.1:8765/v1/chat/completions -H "Authorization: Bearer $KEY" -d @sample-openai.json` returns a real OpenAI SSE stream
- [ ] `curl http://127.0.0.1:8765/admin/quota -H "Authorization: Bearer $ADMIN_KEY"` returns JSON of quota for all accounts
- [ ] On SIGTERM, server shuts down cleanly within 30 seconds, in-flight streams complete or cancel gracefully

### Must Have
- AWS Event Stream binary parser (4-byte total + 4-byte header + 4-byte CRC + headers + payload + CRC), CRC32 validation
- Per-account `sync.Mutex` for token refresh with double-checked locking
- Failure-classified retry: 402/403/429 RECOVERABLE → failover; 400/422/5xx FATAL → return to client
- Anthropic SSE state machine: `message_start` → `content_block_start` → `content_block_delta` (xN) → `content_block_stop` → `message_delta` → `message_stop`
- OpenAI streaming: `chat.completion.chunk` deltas + final chunk with `finish_reason` + `[DONE]` sentinel
- Profile ARN forwarded in `KiroPayload.profileArn`
- Tool/function calling pass-through (input is incrementally accumulated as partial JSON)
- `<thinking>` tag detection and routing (Anthropic native `thinking` block / OpenAI `reasoning_content`)

### Must NOT Have (guardrails)
- **NO mock/stub upstream calls** — all `internal/kiro` code targets real Kiro endpoints. Tests can use `httptest.Server` but production code never bypasses the network.
- **NO TODO/FIXME comments** without GitHub issue references — write the code, do not leave it for later.
- **NO premature abstraction** — interfaces only when there is more than one concrete implementation OR when needed for testing.
- **NO silent error swallowing** — every error is either returned, wrapped, or logged with `log/slog` at WARN+ level.
- **NO config flags that are never read** — every config field must have a code path that uses it.
- **NO IdC / AWS SSO OIDC code** — explicitly out of scope.
- **NO Web UI / dashboard / SPA** — explicitly out of scope.
- **NO conversation compression / response caching / prompt filtering / WebSearch MCP / truncation recovery / model lock** — all explicitly out of scope.
- **NO importing reference repos as Go modules** — they are reference only; Go code is fresh.
- **NO `panic()` in request paths** — recover middleware catches anything, but request handlers return errors.
- **NO global mutable state** — all state lives in injected structs (Server, AccountManager, etc.).
- **NO sleeping in tests longer than 100ms** — use channels / fake clocks.
- **NO un-buffered logging in hot paths** — slog handler must not block request flow.

## Verification Strategy
> **ZERO HUMAN INTERVENTION** — all verification is agent-executed.
- Test framework: stdlib `testing` + `testify` (assert/require)
- Test policy: tests-after; every implementation task includes both unit tests (where applicable) AND agent-executed QA scenarios
- QA tooling: `interactive_bash` for `curl` flows, `Bash` for build/lint/test commands
- Evidence directory: `.sisyphus/evidence/` (created during execution; one file per task)
- Test data: `testdata/` directories alongside packages, with sample Anthropic/OpenAI/Kiro payloads
- AWS Event Stream parser: tested against captured real Kiro response bytes stored in `internal/kiro/testdata/`

## Execution Strategy
### Parallel Execution Waves
> Target: 5-8 tasks per wave. Tasks within a wave have no inter-dependencies.

**Wave 1 — Foundation** (5 tasks parallel)
- T1: Project bootstrap (go.mod, dir tree, .gitignore, Makefile)
- T2: Config package (viper, env + JSON + CLI args)
- T3: Logging package (slog with request ID)
- T4: SQLite migrations + schema
- T5: Errors package (typed errors for classification)

**Wave 2 — Domain Types** (8 tasks parallel, depends on T1)
- T6: Account model + SQLite store CRUD
- T7: Kiro protocol types (KiroPayload, ConversationState, events)
- T8: Anthropic API types
- T9: OpenAI API types
- T10: Normalized intermediate types
- T11: AWS Event Stream binary parser
- T12: Machine ID generator
- T13: Thinking tag FSM parser

**Wave 3 — Auth + Quota + Conversion (input)** (6 tasks parallel, depends on Wave 2)
- T14: Auth Social (refreshToken refresh)
- T15: Auth API Key (no-op refresh)
- T16: Header builder + randomization
- T17: Quota fetcher (getUsageLimits) with cache
- T18: Anthropic ↔ Normalized converter
- T19: OpenAI ↔ Normalized converter

**Wave 4 — Kiro Pipeline** (5 tasks parallel, depends on Wave 3)
- T20: Normalized → KiroPayload converter
- T21: Kiro HTTP client (proxy + retry)
- T22: Anthropic SSE writer
- T23: OpenAI SSE writer
- T24: Kiro stream decoder (binary frames → unified `KiroEvent`)

**Wave 5 — Account Orchestration** (5 tasks parallel, depends on T6/T17/T21)
- T25: Account manager (selection + ownership)
- T26: Load balancer (round_robin, balanced, most_quota)
- T27: Circuit breaker (exponential backoff + probabilistic retry)
- T28: JSON file watcher (fsnotify)
- T29: Kiro client high-level (`request_with_failover`)

**Wave 6 — HTTP Surface** (7 tasks parallel, depends on Wave 4 + Wave 5)
- T30: Gin server skeleton + middleware (auth, recovery, request-id, logging)
- T31: `POST /v1/messages` handler (Anthropic, stream + non-stream)
- T32: `POST /v1/messages/count_tokens`
- T33: `POST /v1/chat/completions` handler (OpenAI, stream + non-stream)
- T34: `GET /v1/models`
- T35: `GET /health`
- T36: Admin `/admin/accounts` CRUD + `/admin/accounts/:id/quota` + `/admin/quota`

**Wave 7 — CLI + Wire-up** (4 tasks parallel, depends on Wave 6)
- T37: cobra CLI commands (account add/list/remove/enable/disable, quota, server)
- T38: `cmd/server/main.go` wire-up + graceful shutdown
- T39: `cmd/cli/main.go` wire-up
- T40: Configuration examples + README

**Final Wave — Verification** (4 tasks parallel)
- F1: Plan compliance audit (oracle)
- F2: Code quality review (unspecified-high)
- F3: Real manual QA (unspecified-high)
- F4: Scope fidelity check (deep)

### Dependency Matrix
| Task | Depends On | Blocks |
|------|------------|--------|
| T1 | — | T2..T40 |
| T2 | T1 | T30, T38 |
| T3 | T1 | T30 |
| T4 | T1 | T6 |
| T5 | T1 | T21, T29 |
| T6 | T1, T4 | T17, T25, T28, T36, T37 |
| T7 | T1 | T20, T24 |
| T8 | T1 | T18, T31, T32 |
| T9 | T1 | T19, T33, T34 |
| T10 | T1 | T18, T19, T20 |
| T11 | T1 | T24 |
| T12 | T1 | T16 |
| T13 | T1 | T22, T23 |
| T14 | T1, T6 | T17, T29 |
| T15 | T1, T6 | T29 |
| T16 | T1, T12 | T17, T21 |
| T17 | T6, T14, T16 | T26, T36 |
| T18 | T8, T10 | T31 |
| T19 | T9, T10 | T33 |
| T20 | T7, T10 | T29 |
| T21 | T5, T16 | T17, T29 |
| T22 | T13 | T31 |
| T23 | T13 | T33 |
| T24 | T7, T11 | T29 |
| T25 | T6 | T26, T29, T36 |
| T26 | T17, T25 | T29 |
| T27 | T25 | T29 |
| T28 | T6 | T38 |
| T29 | T5, T14, T15, T20, T21, T24, T25, T26, T27 | T31, T33 |
| T30 | T2, T3 | T31..T36 |
| T31 | T18, T22, T29, T30 | T38 |
| T32 | T8, T30 | T38 |
| T33 | T19, T23, T29, T30 | T38 |
| T34 | T9, T17, T30 | T38 |
| T35 | T30 | T38 |
| T36 | T6, T17, T25, T30 | T38 |
| T37 | T6, T14, T15, T17 | T39 |
| T38 | T2, T28, T30..T36 | F1..F4 |
| T39 | T37 | F1..F4 |
| T40 | T38, T39 | F1..F4 |

### Agent Dispatch Summary
| Wave | Tasks | Categories Used |
|------|-------|-----------------|
| 1 | T1-T5 | quick, unspecified-low |
| 2 | T6-T13 | unspecified-low, deep (T11 binary parser is non-trivial) |
| 3 | T14-T19 | unspecified-low, deep (T18/T19 conversion correctness matters) |
| 4 | T20-T24 | deep, unspecified-high (T24 stream decoder is hard) |
| 5 | T25-T29 | deep (T29 failover logic), unspecified-high |
| 6 | T30-T36 | unspecified-low, unspecified-high (T31/T33 handlers) |
| 7 | T37-T40 | quick, unspecified-low |
| Final | F1-F4 | oracle, deep, unspecified-high |

## TODOs
> **Implementation + Test = ONE task.** Never separate.
> Every task has Agent Profile + Parallelization + QA Scenarios.

### Wave 1 — Foundation

- [x] **T1. Project bootstrap**

  **What to do**:
  - Create `go.mod` with module path `github.com/irisvn/kiro-let-go`, Go 1.22+
  - Create directory tree exactly per architecture in TL;DR (cmd/, internal/, configs/, docs/, testdata/)
  - Add `.gitignore` (binaries, .data/, .sisyphus/evidence/, *.log, .env)
  - Create `Makefile` with targets: `build`, `vet`, `test`, `lint`, `run`, `clean`
  - Add empty package declarations (`package <name>`) so `go build ./...` works
  - Pin all initial dependencies in go.mod: gin, viper, cobra, modernc.org/sqlite, fsnotify, google/uuid, stretchr/testify, pkoukk/tiktoken-go

  **Must NOT do**: install dependencies eagerly without using them; create files outside the planned tree; touch reference repos.

  **Recommended Agent Profile**:
  - Category: `quick` — Reason: scaffolding only, mechanical work
  - Skills: none — Reason: no domain knowledge required
  - Omitted: `customize-opencode` — not configuring opencode

  **Parallelization**: Can Parallel: NO (foundation) | Wave 1 | Blocks: T2-T40 | Blocked By: none

  **References**:
  - Pattern: `kiro-account-manager/src-tauri/Cargo.toml` (dependency choices)
  - External: https://pkg.go.dev/modernc.org/sqlite (pure-Go SQLite)
  - External: https://gin-gonic.com/docs/quickstart/

  **Acceptance Criteria**:
  - [ ] `go build ./...` exits 0
  - [ ] `go vet ./...` exits 0
  - [ ] `make build` produces `./bin/kiro-let-go` and `./bin/kiro-let-go-cli` (empty main but compiles)
  - [ ] `tree -L 3 .` shows the planned structure

  **QA Scenarios**:
  ```
  Scenario: Clean build from scratch
    Tool: Bash
    Steps: rm -rf bin/ && make build
    Expected: bin/kiro-let-go and bin/kiro-let-go-cli exist; exit code 0
    Evidence: .sisyphus/evidence/task-1-bootstrap.txt
  ```

  **Commit**: YES | Message: `chore(bootstrap): initialize Go module and project structure (T1)` | Files: go.mod, go.sum, Makefile, .gitignore, cmd/**, internal/**

- [x] **T2. Config package**

  **What to do**:
  - Implement `internal/config/config.go` with `Config` struct fields: `Server.Host` (default `0.0.0.0`), `Server.Port` (default `8765`), `Server.AdminAPIKey` (no default — required for admin), `Server.ProxyAPIKey` (required for `/v1/*` access), `Kiro.Region` (default `us-east-1`), `Kiro.AuthRegion` (default `us-east-1`), `Kiro.APIRegion` (default `us-east-1`), `Storage.SQLitePath` (default `.data/kiro.db`), `Storage.CredentialsJSONPath` (optional, enables fsnotify watch), `LoadBalancer.Strategy` (default `round_robin`), `LoadBalancer.StickySession` (default `true`), `Quota.CacheTTLSeconds` (default `43200`), `Failover.BaseCooldownSec` (default `60`), `Failover.MaxBackoffMultiplier` (default `1440`), `Failover.ProbabilisticRetryChance` (default `0.10`), `Failover.MaxAttempts` (default `9`), `Logging.Level` (default `info`), `Logging.Format` (default `json`)
  - Use viper to load from: defaults → `configs/config.json` → env vars (prefix `KIRO_`) → CLI flags
  - Implement `Load(path string) (*Config, error)` and `Validate()` methods
  - Required: `Server.AdminAPIKey` and `Server.ProxyAPIKey` must be non-empty (Validate fails otherwise)

  **Must NOT do**: read os.Environ() directly anywhere outside this package; allow empty admin/proxy keys.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: clear scope, mechanical with one validation step
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: T30, T38 | Blocked By: T1

  **References**:
  - Pattern: `kiro-gateway/kiro/config.py` lines 1-200 (env var conventions, defaults)
  - Pattern: `kiro-account-manager/src-tauri/src/gateway/mod.rs` GatewayConfig struct
  - External: https://github.com/spf13/viper

  **Acceptance Criteria**:
  - [ ] `go test ./internal/config/...` passes
  - [ ] Loading config with missing admin key returns validation error containing "AdminAPIKey"
  - [ ] Env var `KIRO_SERVER_PORT=9999` overrides JSON config
  - [ ] CLI flag `--server.port=7777` overrides env var

  **QA Scenarios**:
  ```
  Scenario: Layered config precedence
    Tool: Bash
    Steps: KIRO_SERVER_PORT=9999 go test ./internal/config/... -run TestLayeredPrecedence -v
    Expected: PASS — CLI > env > JSON > defaults verified
    Evidence: .sisyphus/evidence/task-2-config.txt
  ```

  **Commit**: YES | Message: `feat(config): viper-based layered config loader (T2)` | Files: internal/config/**

- [x] **T3. Logging package**

  **What to do**:
  - Implement `internal/logging/logging.go` exposing `New(cfg config.LoggingConfig) *slog.Logger` and a gin middleware `Middleware(*slog.Logger) gin.HandlerFunc`
  - JSON handler when `Format=json`, text handler when `text`
  - Middleware adds: `request_id` (uuid v4), `method`, `path`, `status`, `duration_ms`, `client_ip`, `user_agent`
  - Inject request ID into `context.Context` via key `logging.RequestIDKey`
  - Helper: `FromContext(ctx) *slog.Logger` returns logger with `request_id` baked in (falls back to default logger)

  **Must NOT do**: use a global logger as default sink; log full request bodies; log Authorization header values.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: standard middleware, but careful redaction
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: T30 | Blocked By: T1

  **References**:
  - External: https://pkg.go.dev/log/slog
  - External: https://gin-gonic.com/docs/examples/custom-middleware/

  **Acceptance Criteria**:
  - [ ] Middleware emits exactly one log line per request
  - [ ] `Authorization` header value is redacted (replaced with `[REDACTED]`)
  - [ ] Request ID propagated via `X-Request-ID` response header

  **QA Scenarios**:
  ```
  Scenario: Sensitive header redaction
    Tool: Bash
    Steps: go test ./internal/logging/... -run TestRedaction -v
    Expected: PASS — log output contains "[REDACTED]" not the secret value
    Evidence: .sisyphus/evidence/task-3-logging.txt
  ```

  **Commit**: YES | Message: `feat(logging): slog logger + gin middleware with redaction (T3)` | Files: internal/logging/**

- [x] **T4. SQLite migrations + schema**

  **What to do**:
  - Implement `internal/account/migrations/0001_init.sql` with these tables (exact schema):
    ```sql
    CREATE TABLE accounts (
      id TEXT PRIMARY KEY,                 -- uuid v4
      label TEXT NOT NULL,
      auth_method TEXT NOT NULL,           -- 'social' | 'apikey'
      access_token TEXT,
      refresh_token TEXT,
      api_key TEXT,                        -- ksk_... for auth_method=apikey
      expires_at TEXT,                     -- RFC3339
      profile_arn TEXT,
      region TEXT NOT NULL DEFAULT 'us-east-1',
      auth_region TEXT,
      api_region TEXT,
      machine_id TEXT NOT NULL,            -- SHA256 hex
      proxy_url TEXT,
      proxy_username TEXT,
      proxy_password TEXT,
      enabled INTEGER NOT NULL DEFAULT 1,
      disabled_reason TEXT,
      failure_count INTEGER NOT NULL DEFAULT 0,
      last_failure_at TEXT,
      success_count INTEGER NOT NULL DEFAULT 0,
      last_used_at TEXT,
      created_at TEXT NOT NULL,
      updated_at TEXT NOT NULL
    );
    CREATE TABLE quota_cache (
      account_id TEXT PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
      payload_json TEXT NOT NULL,
      fetched_at TEXT NOT NULL
    );
    CREATE INDEX idx_accounts_enabled ON accounts(enabled);
    CREATE INDEX idx_accounts_auth_method ON accounts(auth_method);
    ```
  - Embed migrations via `embed.FS`
  - Implement `internal/account/migrate.go` exposing `Apply(ctx, db) error`; tracks applied migrations in `_migrations(version INT PRIMARY KEY, applied_at TEXT)`
  - Idempotent: re-running `Apply` on already-migrated DB is a no-op
  - Open DB with `_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)`

  **Must NOT do**: use `database/sql` Exec without `context.Context`; allow runtime DDL outside migrations.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: well-defined SQL + Go embed
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: T6 | Blocked By: T1

  **References**:
  - External: https://pkg.go.dev/modernc.org/sqlite
  - External: https://www.sqlite.org/pragma.html

  **Acceptance Criteria**:
  - [ ] First `Apply` creates `accounts`, `quota_cache`, `_migrations` tables
  - [ ] Second `Apply` is no-op (verified by row count of `_migrations`)
  - [ ] WAL mode active: `PRAGMA journal_mode` returns `wal`

  **QA Scenarios**:
  ```
  Scenario: Migration idempotence
    Tool: Bash
    Steps: go test ./internal/account/... -run TestMigrationIdempotent -v
    Expected: PASS — second Apply does not re-run migration 0001
    Evidence: .sisyphus/evidence/task-4-migration.txt
  ```

  **Commit**: YES | Message: `feat(account): SQLite schema + idempotent migrations (T4)` | Files: internal/account/migrations/**, internal/account/migrate.go

- [x] **T5. Errors package (typed error classification)**

  **What to do**:
  - Implement `internal/errs/errs.go` with:
    - `Class` enum: `ClassRecoverable`, `ClassFatal`, `ClassQuotaExhausted`, `ClassAuthExpired`, `ClassRateLimited`, `ClassContentTooLong`, `ClassNetwork`, `ClassClientCanceled`
    - `Error` struct: `Class Class`, `Code string`, `HTTPStatus int`, `Message string`, `Cause error` (implements `error` and `Unwrap`)
    - Helpers: `New(class, code, msg)`, `Wrap(err, class, msg)`, `Is(err, class) bool`, `Class(err) Class`
    - Mapper `FromKiroResponse(status int, body []byte) *Error` that classifies per kiro-gateway rules:
      - 200 → not an error
      - 402 + `MONTHLY_REQUEST_COUNT` in body → ClassQuotaExhausted
      - 401/403 → ClassAuthExpired
      - 429 → ClassRateLimited
      - 400 + `CONTENT_LENGTH_EXCEEDS_THRESHOLD` → ClassContentTooLong
      - 400/422 (other) → ClassFatal
      - 5xx → ClassRecoverable (retry-able as transient)
      - else → ClassFatal
    - Mapper `FromNetwork(err error) *Error` covering DNS, refused, TLS, timeout (RECOVERABLE) vs context.Canceled (ClassClientCanceled)

  **Must NOT do**: use `errors.New` for typed errors; bury HTTP body bytes (preserve in Cause where useful).

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: classification logic, well-defined inputs
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 1 | Blocks: T21, T29 | Blocked By: T1

  **References**:
  - Pattern: `kiro-gateway/kiro/account_errors.py` (full classification table)
  - Pattern: `kiro-gateway/kiro/network_errors.py`

  **Acceptance Criteria**:
  - [ ] Table-driven test: 12+ status/body combinations classify correctly
  - [ ] `errs.Is(wrapped, ClassQuotaExhausted)` works through wrapping

  **QA Scenarios**:
  ```
  Scenario: Classification table coverage
    Tool: Bash
    Steps: go test ./internal/errs/... -v
    Expected: PASS — all classification cases verified
    Evidence: .sisyphus/evidence/task-5-errs.txt
  ```

  **Commit**: YES | Message: `feat(errs): typed error classification with Kiro response mapper (T5)` | Files: internal/errs/**

### Wave 2 — Domain Types

- [x] **T6. Account model + SQLite store CRUD**

  **What to do**:
  - Implement `internal/account/model.go` with `Account` struct mirroring the SQLite schema (T4) exactly. Use `time.Time` for timestamps, `*string` for nullable strings.
  - Implement `internal/account/store.go` exposing `Store` struct with constructor `NewStore(db *sql.DB) *Store`
  - Methods (all `(ctx context.Context, ...)`):
    - `Create(acc *Account) error` — generates ID if empty (uuid v4), sets created_at/updated_at, persists machine_id (caller must compute via T12)
    - `Get(id string) (*Account, error)` — returns ErrNotFound if missing
    - `List(filter ListFilter) ([]*Account, error)` — filter by `EnabledOnly bool`, `AuthMethod string`
    - `Update(acc *Account) error` — updates only mutable fields, bumps updated_at
    - `Delete(id string) error`
    - `RecordSuccess(id string) error` — atomic: success_count++, last_used_at=now, failure_count=0
    - `RecordFailure(id string, reason string) error` — atomic: failure_count++, last_failure_at=now
    - `SetEnabled(id string, enabled bool, reason string) error`
    - `UpsertQuota(id string, payload []byte) error` and `GetQuota(id string) (payload []byte, fetchedAt time.Time, err error)`
  - All write methods use `BEGIN IMMEDIATE` transactions
  - Errors wrap to `errs.Error` with appropriate Class

  **Must NOT do**: expose `*sql.DB` outside the package; allow callers to write columns not in the schema; use string concatenation for SQL.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: standard CRUD with prepared statements
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T17, T25, T28, T36, T37 | Blocked By: T1, T4

  **References**:
  - Pattern: `kiro-account-manager/src-tauri/src/core/account.rs` (Account fields)
  - External: https://pkg.go.dev/database/sql
  - External: https://github.com/google/uuid

  **Acceptance Criteria**:
  - [ ] All 9 methods have unit tests using in-memory SQLite (`file::memory:?cache=shared`)
  - [ ] `RecordSuccess` resets failure_count to 0
  - [ ] `RecordFailure` increments atomically (verified with concurrent goroutines)

  **QA Scenarios**:
  ```
  Scenario: Concurrent failure increment
    Tool: Bash
    Steps: go test ./internal/account/... -run TestStoreConcurrency -race -v
    Expected: PASS — 100 goroutines each call RecordFailure once; final count = 100
    Evidence: .sisyphus/evidence/task-6-store.txt

  Scenario: CRUD happy path
    Tool: Bash
    Steps: go test ./internal/account/... -run TestStoreCRUD -v
    Expected: PASS — Create/Get/Update/Delete cycle works
    Evidence: .sisyphus/evidence/task-6-crud.txt
  ```

  **Commit**: YES | Message: `feat(account): SQLite store with CRUD + counters (T6)` | Files: internal/account/model.go, internal/account/store.go, internal/account/store_test.go

- [x] **T7. Kiro protocol types**

  **What to do**:
  - Implement `internal/kiro/types.go` with these structs (JSON-tagged):
    - `KiroPayload { ConversationState ConversationState `json:"conversationState"`; ProfileArn string `json:"profileArn"` }`
    - `ConversationState { ConversationID string; AgentContinuationID string; AgentTaskType string; ChatTriggerType string; CurrentMessage CurrentMessage; History []HistoryItem }`
    - `CurrentMessage { UserInputMessage UserInputMessage `json:"userInputMessage"` }`
    - `UserInputMessage { Content string; ModelID string `json:"modelId"`; Origin string; UserInputMessageContext *UserInputMessageContext `json:"userInputMessageContext,omitempty"`; Images []ImagePart `json:"images,omitempty"` }`
    - `UserInputMessageContext { Tools []Tool `json:"tools,omitempty"`; ToolResults []ToolResult `json:"toolResults,omitempty"` }`
    - `Tool { ToolSpecification ToolSpecification `json:"toolSpecification"` }`
    - `ToolSpecification { Name string; Description string; InputSchema InputSchema `json:"inputSchema"` }`
    - `InputSchema { JSON string `json:"json"` }` (the schema is itself JSON-encoded)
    - `ToolResult { ToolUseID string `json:"toolUseId"`; Content []ToolResultContent; Status string }`
    - `ToolResultContent { Text string }`
    - `HistoryItem` is a sum type — emit either `userInputMessage` or `assistantResponseMessage` per item; implement custom MarshalJSON
    - `AssistantResponseMessage { Content string; ToolUses []ToolUseEntry `json:"toolUses,omitempty"` }`
    - `ToolUseEntry { ToolUseID string `json:"toolUseId"`; Name string; Input string }`
    - `ImagePart { Format string; Source ImageSource }`
    - `ImageSource { Bytes string }` (base64)
  - Event types (decoded payloads):
    - `EventType` const: `EventAssistantResponse`, `EventToolUse`, `EventContextUsage`, `EventMetering`
    - `AssistantResponseEvent { Content string }`
    - `ToolUseEvent { Name string; ToolUseID string `json:"toolUseId"`; Input string; Stop bool }`
    - `ContextUsageEvent { ContextUsagePercentage float64 `json:"contextUsagePercentage"` }`
  - Model ID constants matching kiro.rs mapping:
    - `ModelClaudeSonnet45 = "claude-sonnet-4.5"`, `ModelClaudeSonnet46 = "claude-sonnet-4.6"`, `ModelClaudeOpus45/46/47`, `ModelClaudeHaiku45`
  - Helper `MapModel(input string) string` that maps `*sonnet*4.5` → ModelClaudeSonnet45, etc. (table-driven)

  **Must NOT do**: invent fields not present in Kiro responses; assume `Input` in ToolUseEvent is parsed JSON (it's a partial JSON string fragment).

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: type definitions, well-documented from references
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T20, T24 | Blocked By: T1

  **References**:
  - Pattern: `kiro.rs/src/kiro/model/requests/conversation.rs` and `kiro.rs/src/kiro/model/requests/tool.rs`
  - Pattern: `kiro.rs/src/kiro/model/events/assistant.rs`, `tool_use.rs`, `context_usage.rs`
  - Pattern: `kiro.rs/src/kiro/endpoint/ide.rs` (model name mapping)

  **Acceptance Criteria**:
  - [ ] `json.Marshal(payload)` produces output structurally equivalent to a captured real Kiro request body (snapshot test)
  - [ ] `MapModel` covers all 6 Kiro models + at least 5 alias inputs

  **QA Scenarios**:
  ```
  Scenario: Snapshot match
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestPayloadSnapshot -v
    Expected: PASS — encoded payload matches testdata/payload_expected.json
    Evidence: .sisyphus/evidence/task-7-types.txt
  ```

  **Commit**: YES | Message: `feat(kiro): protocol types and model mapping (T7)` | Files: internal/kiro/types.go, internal/kiro/types_test.go, internal/kiro/testdata/**

- [x] **T8. Anthropic API types**

  **What to do**:
  - Implement `internal/api/anthropic/types.go`:
    - `MessagesRequest { Model string; Messages []Message; System SystemField; MaxTokens int `json:"max_tokens"`; Stream bool; Temperature *float64; TopP *float64 `json:"top_p,omitempty"`; TopK *int `json:"top_k,omitempty"`; StopSequences []string `json:"stop_sequences,omitempty"`; Tools []Tool; ToolChoice *ToolChoice `json:"tool_choice,omitempty"`; Thinking *ThinkingConfig; Metadata map[string]any }`
    - `SystemField` accepts string OR `[]ContentBlock`; implement custom UnmarshalJSON
    - `Message { Role string; Content MessageContent }`
    - `MessageContent` accepts string OR `[]ContentBlock`; custom UnmarshalJSON
    - `ContentBlock` sum type with discriminator `Type`: `text`, `image`, `tool_use`, `tool_result`, `thinking`
    - `Tool { Name string; Description string; InputSchema map[string]any `json:"input_schema"` }`
    - `ToolChoice` discriminator: `auto`, `any`, `tool`, plus `Name` for tool variant
    - `ThinkingConfig { Type string; BudgetTokens int `json:"budget_tokens"` }`
    - `MessagesResponse { ID string; Type string; Role string; Content []ContentBlock; Model string; StopReason string `json:"stop_reason"`; StopSequence *string `json:"stop_sequence"`; Usage Usage }`
    - `Usage { InputTokens int `json:"input_tokens"`; OutputTokens int `json:"output_tokens"` }`
    - SSE event types: `MessageStartEvent`, `ContentBlockStartEvent`, `ContentBlockDeltaEvent`, `ContentBlockStopEvent`, `MessageDeltaEvent`, `MessageStopEvent`, `PingEvent`, `ErrorEvent`
  - `Validate(req *MessagesRequest) error`: model non-empty, max_tokens > 0, messages non-empty, valid roles (`user`/`assistant`)
  - `CountTokensRequest { Model string; Messages []Message; System SystemField; Tools []Tool }`

  **Must NOT do**: assume content is always string; force-coerce to first variant when both shapes possible.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: tedious but mechanical
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T18, T31, T32 | Blocked By: T1

  **References**:
  - External: https://docs.anthropic.com/en/api/messages
  - Pattern: `kiro.rs/src/anthropic/types.rs`
  - Pattern: `kiro-gateway/kiro/models_anthropic.py`

  **Acceptance Criteria**:
  - [ ] Round-trip test: real-world Anthropic request bodies (string-content + block-content forms) marshal/unmarshal without data loss
  - [ ] `Validate` rejects 4+ malformed inputs (empty model, max_tokens=0, no messages, invalid role)

  **QA Scenarios**:
  ```
  Scenario: Polymorphic content unmarshaling
    Tool: Bash
    Steps: go test ./internal/api/anthropic/... -run TestTypesRoundTrip -v
    Expected: PASS — both string and block-content fixtures parse and re-emit equivalent JSON
    Evidence: .sisyphus/evidence/task-8-anthropic-types.txt
  ```

  **Commit**: YES | Message: `feat(anthropic): API types with polymorphic content (T8)` | Files: internal/api/anthropic/types.go, internal/api/anthropic/types_test.go, internal/api/anthropic/testdata/**

- [x] **T9. OpenAI API types**

  **What to do**:
  - Implement `internal/api/openai/types.go`:
    - `ChatCompletionRequest { Model string; Messages []ChatMessage; MaxTokens *int `json:"max_tokens,omitempty"`; Temperature *float64; TopP *float64 `json:"top_p,omitempty"`; Stream bool; Stop StopField; Tools []Tool; ToolChoice ToolChoiceField `json:"tool_choice,omitempty"`; ReasoningEffort *string `json:"reasoning_effort,omitempty"`; ResponseFormat *ResponseFormat `json:"response_format,omitempty"`; User string `json:"user,omitempty"` }`
    - `ChatMessage { Role string; Content MessageContent; Name string `json:",omitempty"`; ToolCallID string `json:"tool_call_id,omitempty"`; ToolCalls []ToolCall `json:"tool_calls,omitempty"` }`
    - `MessageContent` accepts string OR `[]ContentPart` (text/image_url); custom UnmarshalJSON
    - `Tool { Type string; Function FunctionDef }`
    - `FunctionDef { Name string; Description string; Parameters map[string]any }`
    - `ToolCall { ID string; Type string; Function ToolCallFunction }`
    - `ToolCallFunction { Name string; Arguments string }` (Arguments is JSON-encoded string)
    - `ChatCompletionResponse` (non-streaming): `{ ID, Object, Created, Model, Choices []Choice, Usage Usage }`
    - `Choice { Index int; Message ChatMessage; FinishReason string `json:"finish_reason"` }`
    - `Usage { PromptTokens int; CompletionTokens int; TotalTokens int }`
    - `ChatCompletionChunk` (streaming): `{ ID, Object="chat.completion.chunk", Created, Model, Choices []ChunkChoice }`
    - `ChunkChoice { Index int; Delta Delta; FinishReason *string `json:"finish_reason"` }`
    - `Delta { Role string `json:",omitempty"`; Content string `json:",omitempty"`; ReasoningContent string `json:"reasoning_content,omitempty"`; ToolCalls []ChunkToolCall `json:"tool_calls,omitempty"` }`
    - `ChunkToolCall { Index int; ID string `json:",omitempty"`; Type string `json:",omitempty"`; Function ChunkFunction }`
    - `ModelsResponse { Object="list"; Data []ModelInfo }`
    - `ModelInfo { ID, Object="model", Created int64, OwnedBy string }`
  - `Validate(req)`: model non-empty, messages non-empty, valid role (`system`/`user`/`assistant`/`tool`), tool messages have `tool_call_id`

  **Must NOT do**: force `Arguments` to be parsed JSON (it must remain a JSON-string for streaming-correctness).

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: mechanical types
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T19, T33, T34 | Blocked By: T1

  **References**:
  - External: https://platform.openai.com/docs/api-reference/chat
  - Pattern: `kiro-gateway/kiro/models_openai.py`

  **Acceptance Criteria**:
  - [ ] Round-trip test: 4 fixtures (string-content, multimodal, tool-call, tool-result) parse & re-emit
  - [ ] `Validate` rejects 4+ malformed inputs

  **QA Scenarios**:
  ```
  Scenario: Tool call streaming chunk shape
    Tool: Bash
    Steps: go test ./internal/api/openai/... -run TestChunkShape -v
    Expected: PASS — encoded chunk matches OpenAI reference fixture exactly
    Evidence: .sisyphus/evidence/task-9-openai-types.txt
  ```

  **Commit**: YES | Message: `feat(openai): API types for chat completions (T9)` | Files: internal/api/openai/types.go, internal/api/openai/types_test.go, internal/api/openai/testdata/**

- [x] **T10. Normalized intermediate types**

  **What to do**:
  - Implement `internal/converter/normalized.go` with the unified intermediate format used by both Anthropic and OpenAI converters:
    - `NormalizedRequest { Model string; SystemPrompt string; Messages []NormalizedMessage; Tools []NormalizedTool; ToolChoice NormalizedToolChoice; MaxOutputTokens int; Temperature *float64; TopP *float64; TopK *int; Stop []string; ThinkingBudget int /* 0 = disabled */; Stream bool; ReasoningEffort string /* "", "minimal", "low", "medium", "high", "xhigh" */ }`
    - `NormalizedMessage { Role string /* "user"|"assistant"|"tool" */; Parts []NormalizedPart }`
    - `NormalizedPart` sum type with discriminator: `Text{Text string}`, `Image{MediaType string; DataB64 string}`, `ToolUse{ID, Name, InputJSON string}`, `ToolResult{ToolUseID, ContentText string, IsError bool}`, `Thinking{Text, Signature string}`
    - `NormalizedTool { Name string; Description string; SchemaJSON string }` (schema as JSON-encoded string for forwarding)
    - `NormalizedToolChoice { Mode string /* "auto"|"any"|"none"|"tool" */; Name string }`
  - Response intermediate (used by both `from_kiro` and the SSE writers):
    - `NormalizedChunk` sum type: `TextDelta{Text}`, `ThinkingDelta{Text}`, `ToolUseStart{ID, Name}`, `ToolUseDelta{ID, InputDelta}`, `ToolUseStop{ID}`, `Usage{InputTokens, OutputTokens}`, `ContextUsage{Pct}`, `Stop{Reason string}`

  **Must NOT do**: leak format-specific (Anthropic/OpenAI) field names into normalized types; assume tools always present.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: type design
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T18, T19, T20 | Blocked By: T1

  **References**:
  - Pattern: `kiro-account-manager/src-tauri/src/gateway/models.rs` (`NormalizedRequest`)
  - Pattern: `kiro-gateway/kiro/converters_core.py`

  **Acceptance Criteria**:
  - [ ] Sum-type pattern: each `NormalizedPart`/`NormalizedChunk` variant constructs cleanly and switch-statements compile-check exhaustive cases via a dedicated `Kind()` method

  **QA Scenarios**:
  ```
  Scenario: Variant kind round-trip
    Tool: Bash
    Steps: go test ./internal/converter/... -run TestNormalizedKinds -v
    Expected: PASS — every variant has unique Kind() and round-trips through helper constructors
    Evidence: .sisyphus/evidence/task-10-normalized.txt
  ```

  **Commit**: YES | Message: `feat(converter): normalized intermediate types (T10)` | Files: internal/converter/normalized.go, internal/converter/normalized_test.go

- [x] **T11. AWS Event Stream binary parser**

  **What to do**:
  - Implement `internal/kiro/eventstream.go` with a streaming parser:
    - Frame layout: `[total_length u32][headers_length u32][prelude_crc u32][headers ...][payload ...][message_crc u32]`
    - Header layout: `[name_length u8][name bytes][value_type u8][value ...]` per header
    - Value types per AWS Event Stream spec: 0=BoolTrue (0 bytes), 1=BoolFalse, 2=Byte (1), 3=Short (2), 4=Integer (4), 5=Long (8), 6=ByteArray (u16-len), 7=String (u16-len), 8=Timestamp (i64), 9=UUID (16), 10=BoolTrue alias (different per AWS docs — verify against kiro.rs reference)
    - Parser interface:
      - `type Parser struct { ... }` with `Feed(data []byte) ([]Frame, error)` (yields complete frames; stores partial buffer between calls)
      - `Frame { Headers map[string]string; Payload []byte }`
      - Validates `prelude_crc` (CRC32 of first 8 bytes) and `message_crc` (CRC32 of total_length..end-of-payload)
      - On CRC mismatch: return error, drop frame, attempt recovery by scanning forward
    - Use `hash/crc32` with Castagnoli table (CRC32C — verify with kiro.rs `kiro/parser/crc.rs`)
  - Event-type dispatcher:
    - `func DecodeEvent(headers map[string]string, payload []byte) (kind EventType, value any, err error)`
    - Routes by `:event-type` header to `AssistantResponseEvent` / `ToolUseEvent` / `ContextUsageEvent` / `MeteringEvent`
    - Handles `:message-type=error` and `:message-type=exception` by returning `errs.Error` (Class FATAL or RECOVERABLE based on `:error-code`)

  **Must NOT do**: read the entire response into memory; use json.Unmarshal on the full byte stream; ignore CRC failures.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: binary protocol, byte-level correctness, CRC validation, recovery logic
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T24 | Blocked By: T1

  **References**:
  - Pattern: `kiro.rs/src/kiro/parser/decoder.rs`, `frame.rs`, `header.rs`, `crc.rs`
  - External: https://docs.aws.amazon.com/AmazonS3/latest/API/RESTSelectObjectAppendix.html (Event Stream spec)
  - Test fixture: capture a real Kiro response into `internal/kiro/testdata/sample_stream.bin` (will be added at T11 time from an actual run; if unavailable, hand-craft one matching the spec)

  **Acceptance Criteria**:
  - [ ] Decodes a captured/hand-crafted multi-frame sample with text + tool_use + context_usage events correctly
  - [ ] CRC32C validation rejects a corrupted frame and resyncs to next valid frame
  - [ ] Handles split-across-Feed-calls input (10-byte chunks)

  **QA Scenarios**:
  ```
  Scenario: Multi-frame decode + chunk resilience
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestEventStream -v
    Expected: PASS — frames decoded identically whether input is delivered whole or in 10-byte chunks
    Evidence: .sisyphus/evidence/task-11-eventstream.txt

  Scenario: Corrupted frame recovery
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestEventStreamCRC -v
    Expected: PASS — corrupted middle frame skipped, subsequent valid frame still decoded
    Evidence: .sisyphus/evidence/task-11-eventstream-crc.txt
  ```

  **Commit**: YES | Message: `feat(kiro): AWS Event Stream binary parser with CRC validation (T11)` | Files: internal/kiro/eventstream.go, internal/kiro/eventstream_test.go, internal/kiro/testdata/sample_stream.bin

- [x] **T12. Machine ID generator**

  **What to do**:
  - Implement `internal/kiro/machine_id.go` exposing `Generate(seed string) string`:
    - Returns SHA256 hex of `seed` concatenated with a constant salt `"KiroIDE-MachineID-v1"`, lowercased
    - For account creation: caller passes a stable seed (account email, or random uuid if no email), persists the result in `accounts.machine_id` column. The same account always reuses the stored machine_id; never regenerated.
  - Helper `Validate(id string) error` checks 64-char hex

  **Must NOT do**: use `os.Hostname` or any host-derived value; randomize per-request.

  **Recommended Agent Profile**:
  - Category: `quick` — Reason: trivial implementation
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T16 | Blocked By: T1

  **References**:
  - Pattern: `kiro.rs/src/kiro/machine_id.rs`

  **Acceptance Criteria**:
  - [ ] Same seed → same id (deterministic)
  - [ ] Different seeds → different ids
  - [ ] Output is 64 hex characters

  **QA Scenarios**:
  ```
  Scenario: Determinism
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestMachineID -v
    Expected: PASS
    Evidence: .sisyphus/evidence/task-12-machineid.txt
  ```

  **Commit**: YES | Message: `feat(kiro): per-account machine ID generator (T12)` | Files: internal/kiro/machine_id.go, internal/kiro/machine_id_test.go

- [x] **T13. Thinking tag FSM parser**

  **What to do**:
  - Implement `internal/thinking/parser.go` exposing a streaming parser that processes appended chunks of model output and emits `(visibleText, thinkingText)` deltas:
    - States: `PreContent` (only whitespace/empty seen), `InThinking` (inside `<thinking>...</thinking>`), `Streaming` (past thinking)
    - Recognized opening tags: `<thinking>`, `<think>`, `<reasoning>`, `<thought>` — only when seen at very start of the response (PreContent → InThinking transition)
    - Closing tag must match the opening tag; on close, transition to Streaming
    - "Cautious" buffering: hold up to N bytes (where N = max-tag-length+2) when output ends mid-tag candidate
    - Methods:
      - `(p *Parser) Feed(chunk string) (visible, thinking string)`
      - `(p *Parser) Close() (visible, thinking string)` — flushes buffered bytes
    - Configuration: `Mode` enum: `ModeRouteThinking` (split into thinking/visible), `ModeStripTags` (drop tags but keep content as visible), `ModePassThrough` (no parsing)

  **Must NOT do**: split a tag across two emit calls (must always emit-on-tag-boundary); modify content outside thinking blocks.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: state machine correctness, tricky boundary conditions
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 2 | Blocks: T22, T23 | Blocked By: T1

  **References**:
  - Pattern: `kiro-gateway/kiro/thinking_parser.py` (FSM design + cautious buffering)
  - Pattern: `kiro.rs/src/anthropic/stream.rs` (boundary detection)

  **Acceptance Criteria**:
  - [ ] Table-driven test with 12+ inputs covering: chunk-split tags, no-tag content, multiple tag variants, malformed (open without close)
  - [ ] Total visible+thinking output equals total input minus tag literals (for valid inputs)

  **QA Scenarios**:
  ```
  Scenario: Tag split across chunks
    Tool: Bash
    Steps: go test ./internal/thinking/... -run TestParserChunkSplit -v
    Expected: PASS — same output regardless of chunk boundaries
    Evidence: .sisyphus/evidence/task-13-thinking.txt
  ```

  **Commit**: YES | Message: `feat(thinking): FSM tag parser with cautious buffering (T13)` | Files: internal/thinking/parser.go, internal/thinking/parser_test.go

### Wave 3 — Auth + Quota + Conversion (input)

- [x] **T14. Auth Social (refreshToken refresh)**

  **What to do**:
  - Implement `internal/kiro/auth_social.go`:
    - `type SocialAuth struct { httpClient *http.Client; logger *slog.Logger }`
    - Method `Refresh(ctx context.Context, acc *account.Account) (newAccessToken string, newRefreshToken string, expiresAt time.Time, err error)`:
      - URL: `https://prod.{authRegion}.auth.desktop.kiro.dev/refreshToken` (resolves authRegion via `acc.AuthRegion ?? acc.Region ?? "us-east-1"`)
      - Method POST, Content-Type: application/json
      - Body: `{"refreshToken":"<acc.RefreshToken>"}`
      - Required headers: `User-Agent: KiroIDE-{kiroVersion}-{acc.MachineID}`, `host: prod.{authRegion}.auth.desktop.kiro.dev`, `Connection: close`
      - On 200: parse `{accessToken, refreshToken?, profileArn?, expiresIn?}`. Compute expiresAt = now + expiresIn (default 3600s if absent). If `refreshToken` rotates, return new value (caller persists).
      - On 400 with `invalid_grant` + `Invalid refresh token provided` → return `errs.New(ClassFatal, "INVALID_REFRESH_TOKEN", ...)` so caller permanently disables the account
      - On 401/403 → ClassFatal
      - On 429/5xx → ClassRecoverable (caller may retry or move on)
    - Use the proxy if `acc.ProxyURL` set (build per-call client via T21 helper)
  - `KiroVersion` constant (in `internal/kiro/version.go`): `"1.0.34"` (matches kiro.rs default; configurable via env `KIRO_IDE_VERSION` for randomization in T16)

  **Must NOT do**: cache refresh results across accounts; reuse a global http.Client for proxied accounts; log token bodies.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: clear request/response, classification matters
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: T17, T29 | Blocked By: T1, T6

  **References**:
  - Pattern: `kiro.rs/src/kiro/token_manager.rs` (refresh logic + endpoints)
  - Pattern: `kiro-gateway/kiro/auth.py` (Kiro Desktop API)

  **Acceptance Criteria**:
  - [ ] `httptest.Server` mocks Kiro auth endpoint; happy path returns parsed tokens
  - [ ] 400 + invalid_grant → ClassFatal with code `INVALID_REFRESH_TOKEN`
  - [ ] Proxy URL is honored (mock proxy seen in request)

  **QA Scenarios**:
  ```
  Scenario: Refresh happy path + permanent failure
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestSocialAuth -v
    Expected: PASS — both fixtures classify correctly
    Evidence: .sisyphus/evidence/task-14-auth-social.txt
  ```

  **Commit**: YES | Message: `feat(kiro): Social auth refresh via Kiro Desktop API (T14)` | Files: internal/kiro/auth_social.go, internal/kiro/auth_social_test.go

- [x] **T15. Auth API Key (no-op refresh)**

  **What to do**:
  - Implement `internal/kiro/auth_apikey.go`:
    - `type APIKeyAuth struct{}`
    - Method `Refresh(ctx, acc) (token string, expiresAt time.Time, err error)` returns `acc.APIKey, time.Now().Add(100*365*24*time.Hour), nil` (effectively never expires)
    - Validate API key shape: must start with `ksk_` (return ClassFatal with code `INVALID_API_KEY` otherwise)

  **Must NOT do**: hit any network endpoint; cache anything.

  **Recommended Agent Profile**:
  - Category: `quick` — Reason: trivial
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: T29 | Blocked By: T1, T6

  **References**:
  - Pattern: `kiro.rs/src/kiro/token_manager.rs` (API key handling)

  **Acceptance Criteria**:
  - [ ] Returns the stored API key unchanged
  - [ ] Rejects keys not starting with `ksk_`

  **QA Scenarios**:
  ```
  Scenario: API key shape validation
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestAPIKeyAuth -v
    Expected: PASS
    Evidence: .sisyphus/evidence/task-15-auth-apikey.txt
  ```

  **Commit**: YES | Message: `feat(kiro): API key auth with shape validation (T15)` | Files: internal/kiro/auth_apikey.go, internal/kiro/auth_apikey_test.go

- [x] **T16. Header builder + randomization**

  **What to do**:
  - Implement `internal/antiban/headers.go` exposing `BuildKiroRequestHeaders(acc *account.Account, region string) http.Header`:
    - Required headers (always present, exact values):
      - `Authorization: Bearer <accessToken or apikey>`
      - `Content-Type: application/json`
      - `Connection: close`
      - `host: q.{region}.amazonaws.com`
      - `x-amzn-codewhisperer-optout: true`
      - `x-amzn-kiro-agent-mode: vibe`
      - `amz-sdk-invocation-id: <fresh uuid v4 per request>`
      - `amz-sdk-request: attempt=1; max=3`
      - `tokentype: API_KEY` (only when auth_method=apikey)
    - Randomized headers:
      - `User-Agent`: pick from a curated list of valid Kiro IDE UA strings (3-5 versions, e.g., `aws-sdk-js/1.0.34 ua/2.1 os/{os} lang/js md/nodejs#v20.10.0 api/codewhispererstreaming#1.0.34 m/E KiroIDE-{ver}-{machineId}`). The chosen `ver` is derived from a stable hash of `acc.ID` so the SAME account always shows the SAME UA string (never random per request, that itself looks suspicious).
      - `x-amz-user-agent`: matching short form `aws-sdk-js/{ver} KiroIDE-{ver}-{machineId}`
      - The OS portion (`os/...`) is also stable per account: hashed selection from `darwin`, `linux`, `win32`
    - Helper `OnceFor(accountID string, listLen int) int` — deterministic stable selection

  **Must NOT do**: randomize User-Agent per request (that pattern is the anomaly); use real OS detection; embed PII.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: anti-ban heuristic correctness matters
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: T17, T21 | Blocked By: T1, T12

  **References**:
  - Pattern: `kiro.rs/src/kiro/endpoint/ide.rs` (header construction)
  - Pattern: `kiro-account-manager/src-tauri/src/clients/http_client.rs` (UA conventions)

  **Acceptance Criteria**:
  - [ ] Same `acc.ID` produces same UA across 100 calls (stability test)
  - [ ] `amz-sdk-invocation-id` is a fresh UUID per call (uniqueness test)
  - [ ] All 9 required headers present

  **QA Scenarios**:
  ```
  Scenario: Header stability + per-request UUID
    Tool: Bash
    Steps: go test ./internal/antiban/... -run TestHeaders -v
    Expected: PASS — UA stable per account, invocation-id always unique
    Evidence: .sisyphus/evidence/task-16-headers.txt
  ```

  **Commit**: YES | Message: `feat(antiban): per-account stable header randomization (T16)` | Files: internal/antiban/headers.go, internal/antiban/headers_test.go

- [x] **T17. Quota fetcher (getUsageLimits) with cache**

  **What to do**:
  - Implement `internal/account/quota.go`:
    - `type Quota struct { SubscriptionTitle string; LimitTotal int64; LimitRemaining int64; ResetTime time.Time; Raw json.RawMessage; FetchedAt time.Time }`
    - `type Fetcher struct { httpClient *http.Client; store *Store; ttl time.Duration; logger *slog.Logger }`
    - Method `Get(ctx, acc, force bool) (*Quota, error)`:
      - If `!force` and cached entry in `quota_cache` is fresher than `ttl`, return cached
      - Otherwise GET `https://q.{apiRegion}.amazonaws.com/getUsageLimits`
      - Headers from T16 (no payload)
      - Response shape: parse fields per `kiro-gateway/kiro/account_manager.py` and `kiro-account-manager/src-tauri/src/clients/kiro_q_client.rs`. The Kiro response includes a `usageBreakdownList` and `subscriptionTitle`; treat the entire body as `Raw json.RawMessage` and extract `subscriptionTitle`, `monthlyRequestCount`, `monthlyRequestLimit` if present.
      - Persist via `store.UpsertQuota`
    - Method `Summary(ctx) ([]*QuotaSummaryItem, error)`: returns one item per account with cached quota or fresh
    - **Probe avoidance**: never auto-fetch on a schedule; only fetch when:
      - Caller explicitly asks (admin endpoint, CLI command)
      - On-demand from balancer when strategy=most_quota and cache empty/stale
      - On 402 quota error (force-refresh once)

  **Must NOT do**: poll quota in a background goroutine (that's a probe pattern); spam the endpoint.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: cache invalidation logic is subtle, anti-probe coupling
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: T26, T36 | Blocked By: T6, T14, T16

  **References**:
  - Pattern: `kiro-account-manager/src-tauri/src/clients/kiro_q_client.rs` (`getUsageLimits`)
  - Pattern: `kiro-gateway/kiro/account_manager.py` (`ACCOUNT_CACHE_TTL`)

  **Acceptance Criteria**:
  - [ ] Cache hit within TTL skips network (verified via httptest counter)
  - [ ] `force=true` bypasses cache
  - [ ] No background goroutine starts (verified by goroutine leak detector)

  **QA Scenarios**:
  ```
  Scenario: Cache TTL + forced refresh
    Tool: Bash
    Steps: go test ./internal/account/... -run TestQuotaFetcher -v
    Expected: PASS — exactly 1 network call across 5 cached calls; force triggers 2nd
    Evidence: .sisyphus/evidence/task-17-quota.txt
  ```

  **Commit**: YES | Message: `feat(account): quota fetcher with TTL cache and probe avoidance (T17)` | Files: internal/account/quota.go, internal/account/quota_test.go

- [x] **T18. Anthropic ↔ Normalized converter**

  **What to do**:
  - Implement `internal/converter/anthropic.go`:
    - `AnthropicToNormalized(req *anthropic.MessagesRequest) (*NormalizedRequest, error)`:
      - System: if string, that's `SystemPrompt`; if blocks, concatenate `text` blocks in order
      - Messages: each block → `NormalizedPart` (text→Text, image→Image, tool_use→ToolUse, tool_result→ToolResult, thinking→Thinking)
      - Tools: each `Tool` → `NormalizedTool { Name, Description, SchemaJSON: json.Marshal(InputSchema) }`
      - ToolChoice: `auto`/`any`/`none`/`tool` mapping
      - Thinking: if `req.Thinking != nil && type=="enabled"` → `ThinkingBudget = req.Thinking.BudgetTokens` (default 4000 if 0)
      - Stop sequences, temperature, top_p, top_k, max_tokens forwarded
    - `NormalizedToAnthropic(resp normalized.Response) (*anthropic.MessagesResponse, error)` — used for non-streaming response assembly
    - Streaming side handled in T22 (SSE writer)

  **Must NOT do**: drop tool-related parts; force `system` to a single string when it has cache_control blocks (preserve text content only).

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: conversion correctness directly affects user-visible behavior
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: T31 | Blocked By: T8, T10

  **References**:
  - Pattern: `kiro-gateway/kiro/converters_anthropic.py`
  - Pattern: `kiro.rs/src/anthropic/converter.rs`

  **Acceptance Criteria**:
  - [ ] 4 fixtures convert without information loss (text-only, with-tools, with-thinking, multimodal)
  - [ ] Round-trip: AnthropicToNormalized → NormalizedToAnthropic for non-streaming case preserves content blocks

  **QA Scenarios**:
  ```
  Scenario: Fixture-driven conversion
    Tool: Bash
    Steps: go test ./internal/converter/... -run TestAnthropicConvert -v
    Expected: PASS — all 4 fixtures match expected normalized form
    Evidence: .sisyphus/evidence/task-18-anthropic-convert.txt
  ```

  **Commit**: YES | Message: `feat(converter): Anthropic↔Normalized conversion (T18)` | Files: internal/converter/anthropic.go, internal/converter/anthropic_test.go, internal/converter/testdata/anthropic/**

- [x] **T19. OpenAI ↔ Normalized converter**

  **What to do**:
  - Implement `internal/converter/openai.go`:
    - `OpenAIToNormalized(req *openai.ChatCompletionRequest) (*NormalizedRequest, error)`:
      - Extract `system` messages (concatenate content) into `SystemPrompt`
      - Map remaining messages: `user`/`assistant`/`tool` → respective normalized roles
      - Multimodal `content_part` `image_url` → `NormalizedPart{Image}` (data URL only — base64-extract; HTTP URLs unsupported, return ClassFatal `IMAGE_URL_UNSUPPORTED`)
      - `tool_calls` on assistant message → `NormalizedPart{ToolUse}` entries
      - `tool` role messages → `NormalizedPart{ToolResult}` (paired by `tool_call_id`)
      - Tools: `req.Tools[].Function` → `NormalizedTool`, `Parameters` → `SchemaJSON`
      - `ReasoningEffort` mapped to `ThinkingBudget`: none/minimal=0, low=2000, medium=4000, high=8000, xhigh=10000
      - Stop sequences, temperature, top_p, max_tokens forwarded
      - Adjacent same-role messages merged
    - `NormalizedToOpenAI(resp)` for non-streaming assembly
    - Streaming in T23

  **Must NOT do**: parse `tool_calls[].function.arguments` as JSON (must remain string for streaming compatibility); reorder tool messages.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: like T18, correctness matters
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 3 | Blocks: T33 | Blocked By: T9, T10

  **References**:
  - Pattern: `kiro-gateway/kiro/converters_openai.py`

  **Acceptance Criteria**:
  - [ ] 4 fixtures convert correctly (chat, with-tools, with-tool-result, multimodal data URL)
  - [ ] HTTP image URL rejected with `IMAGE_URL_UNSUPPORTED`

  **QA Scenarios**:
  ```
  Scenario: Fixture-driven conversion
    Tool: Bash
    Steps: go test ./internal/converter/... -run TestOpenAIConvert -v
    Expected: PASS — all 4 fixtures match
    Evidence: .sisyphus/evidence/task-19-openai-convert.txt
  ```

  **Commit**: YES | Message: `feat(converter): OpenAI↔Normalized conversion (T19)` | Files: internal/converter/openai.go, internal/converter/openai_test.go, internal/converter/testdata/openai/**

### Wave 4 — Kiro Pipeline

- [x] **T20. Normalized → KiroPayload converter**

  **What to do**:
  - Implement `internal/converter/to_kiro.go`:
    - `NormalizedToKiro(req *NormalizedRequest, profileArn string) (*kiro.KiroPayload, error)`:
      - Map `req.Model` via `kiro.MapModel`
      - Generate `conversationId` (uuid v4) and `agentContinuationId` (uuid v4); set `agentTaskType="vibe"`, `chatTriggerType="MANUAL"`
      - Build `History`: all messages except the last user message become history items (alternating user/assistant)
      - Adjacent assistant messages with tool_use parts collapse into one `AssistantResponseMessage` with combined `ToolUses`
      - Tool results (role=tool) become a `UserInputMessage` with `userInputMessageContext.toolResults`
      - Last user message → `CurrentMessage.UserInputMessage`:
        - `Content`: concatenate all text parts; if thinking budget enabled, prepend `<thinking_mode>extended</thinking_mode><max_thinking_length>{N}</max_thinking_length>` (matches kiro-gateway tag injection)
        - `ModelID`: mapped model
        - `Origin`: `"AI_EDITOR"`
        - `UserInputMessageContext.Tools`: convert `NormalizedTool` to `Tool{ToolSpecification{Name, Description, InputSchema:{JSON: schemaJSON}}}`
        - `Images`: from any `Image` parts
      - Validate tool_use/tool_result pairing: any orphan ToolUse without a matching ToolResult in next user turn (and vice versa) → drop both with a WARN log (matches kiro.rs `Content pairing validation`)
    - Helper `validatePairing(history []HistoryItem, current *CurrentMessage) (cleanedHistory, cleanedCurrent, droppedIDs)`

  **Must NOT do**: rewrite tool_use IDs; mutate inputs (always return a fresh KiroPayload); inject Kiro-specific structures outside this converter.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: conversion is the most failure-prone step; pairing validation has subtle cases
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: T29 | Blocked By: T7, T10

  **References**:
  - Pattern: `kiro.rs/src/anthropic/converter.rs` (history+current split, pairing logic)
  - Pattern: `kiro-gateway/kiro/converters_core.py` (system→guardContent, tool injection)

  **Acceptance Criteria**:
  - [ ] 5 fixtures (text-only, with-tools, with-tool-result, with-thinking, multi-turn) produce expected KiroPayload (snapshot match)
  - [ ] Orphan tool_use is dropped with WARN log

  **QA Scenarios**:
  ```
  Scenario: Snapshot match across fixtures
    Tool: Bash
    Steps: go test ./internal/converter/... -run TestToKiro -v
    Expected: PASS — encoded KiroPayload matches expected fixtures
    Evidence: .sisyphus/evidence/task-20-to-kiro.txt
  ```

  **Commit**: YES | Message: `feat(converter): Normalized→KiroPayload with pairing validation (T20)` | Files: internal/converter/to_kiro.go, internal/converter/to_kiro_test.go, internal/converter/testdata/kiro/**

- [x] **T21. Kiro HTTP client (proxy + retry)**

  **What to do**:
  - Implement `internal/kiro/client.go`:
    - `type Client struct { baseClient *http.Client; logger *slog.Logger }`
    - Constructor `NewClient(timeoutSec int, logger) *Client` — base client with `Timeout: 0` (streaming-friendly), `Transport: &http.Transport{ ... DisableKeepAlives: true }` (matches kiro.rs `Connection: close`)
    - Helper `clientForAccount(acc *account.Account) *http.Client`:
      - If `acc.ProxyURL` set, build a NEW client with that proxy: parse URL, support http/https/socks5, embed username/password
      - Otherwise return `c.baseClient`
      - Cache per-account client in a `sync.Map` keyed by account.ID, invalidate on account update
    - Method `Do(ctx, acc, req *http.Request) (*http.Response, error)`: applies the per-account client; sets `req.Body = http.NoBody` for GET; preserves stream-ability for POST
    - Method `Stream(ctx, acc, req *http.Request) (io.ReadCloser, *http.Response, error)`: wraps Do but ensures response body is suitable for streaming (no `gzip` auto-decode unless `Accept-Encoding: identity`); returns body that caller MUST Close
    - Apply retry inside (NOT failover — that's T29):
      - 429: backoff 1s, 2s, 4s capped at 3 attempts (per attempt; total per-account)
      - 5xx: same as 429
      - timeout/dns/refused: same as 429
      - 200/4xx (non-429): return immediately
    - All errors classified via T5

  **Must NOT do**: implement failover here (T29's responsibility); decode body on the caller's behalf; reuse same `*http.Client` across accounts with different proxies.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: streaming + retry + proxy interactions are tricky
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: T17, T29 | Blocked By: T5, T16

  **References**:
  - Pattern: `kiro-gateway/kiro/http_client.py` (request_with_retry)
  - External: https://pkg.go.dev/net/http
  - External: https://pkg.go.dev/golang.org/x/net/proxy (for SOCKS5)

  **Acceptance Criteria**:
  - [ ] 429 → 1 retry then return last response (verified counter)
  - [ ] Proxy URL honored: `httptest`-based proxy receives request first
  - [ ] Streaming response body NOT consumed before returning to caller

  **QA Scenarios**:
  ```
  Scenario: Per-account proxy + retry behavior
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestClient -v
    Expected: PASS — proxy honored, 429 retry, 200 short-circuits
    Evidence: .sisyphus/evidence/task-21-client.txt
  ```

  **Commit**: YES | Message: `feat(kiro): HTTP client with per-account proxy and retry (T21)` | Files: internal/kiro/client.go, internal/kiro/client_test.go

- [x] **T22. Anthropic SSE writer**

  **What to do**:
  - Implement `internal/api/anthropic/sse.go`:
    - `type SSEWriter struct { w io.Writer; flusher http.Flusher; mu sync.Mutex; messageID string; model string; sentMessageStart bool; openBlocks map[int]string /* index → type */; outputTokens int }`
    - Methods (all serialize via mu, flush after each event):
      - `WriteMessageStart(inputTokens int)` — emits `event: message_start` + `data: {...}`. Includes initial `message` object with empty content array.
      - `WriteContentBlockStart(index int, blockType string /* "text"|"thinking"|"tool_use" */, optionalToolID, optionalToolName string)` — emits `event: content_block_start` + start frame
      - `WriteContentBlockDelta(index int, delta DeltaPart)` where `DeltaPart` is `TextDelta | ThinkingDelta | InputJSONDelta` — emits `content_block_delta`
      - `WriteContentBlockStop(index int)` — emits `content_block_stop`
      - `WriteMessageDelta(stopReason string)` — emits `message_delta` with `usage.output_tokens`
      - `WriteMessageStop()` — emits `message_stop`
      - `WritePing()` — emits `event: ping`
      - `WriteError(err *errs.Error)` — emits `event: error` with shape `{"type":"error","error":{"type":"<class>","message":"..."}}`
    - State enforcement: `WriteContentBlockDelta(index)` rejects (returns error) if `WriteContentBlockStart(index)` not called or `WriteContentBlockStop(index)` already called
    - `WriteMessageStart` only succeeds once
    - Routes `<thinking>` from T13 parser into `index=0` thinking blocks; visible text into `index=1+` text blocks
    - Caller is responsible for setting headers `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive` BEFORE first write

  **Must NOT do**: buffer events (must flush immediately); allow concurrent writes from multiple goroutines without mu; auto-close blocks (caller must explicitly stop).

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: SSE state machine correctness, concurrency
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: T31 | Blocked By: T13

  **References**:
  - Pattern: `kiro.rs/src/anthropic/stream.rs` (SseStateManager)
  - External: https://docs.anthropic.com/en/api/messages-streaming

  **Acceptance Criteria**:
  - [ ] Writing message_start twice returns error
  - [ ] Writing delta to unopened block returns error
  - [ ] Output for a captured fixture (text + tool_use + thinking + stop) matches expected SSE bytes line-for-line

  **QA Scenarios**:
  ```
  Scenario: SSE event order + state enforcement
    Tool: Bash
    Steps: go test ./internal/api/anthropic/... -run TestSSEWriter -v
    Expected: PASS — order enforced, fixture matches
    Evidence: .sisyphus/evidence/task-22-anthropic-sse.txt
  ```

  **Commit**: YES | Message: `feat(anthropic): SSE writer with state machine (T22)` | Files: internal/api/anthropic/sse.go, internal/api/anthropic/sse_test.go

- [x] **T23. OpenAI SSE writer**

  **What to do**:
  - Implement `internal/api/openai/sse.go`:
    - `type SSEWriter struct { w io.Writer; flusher http.Flusher; mu sync.Mutex; id, model string; created int64; toolCallIndex map[string]int /* toolUseID → index */; nextIndex int }`
    - Methods:
      - `WriteContentDelta(text string)` — emits one `chat.completion.chunk` with `delta.content`
      - `WriteReasoningDelta(text string)` — emits chunk with `delta.reasoning_content` (only when client requested via `reasoning_effort` or thinking present)
      - `WriteToolCallStart(toolUseID, name string)` — first chunk for the tool call: assigns index, emits `delta.tool_calls=[{index, id, type:"function", function:{name, arguments:""}}]`
      - `WriteToolCallArgsDelta(toolUseID string, argsDelta string)` — incremental Args
      - `WriteToolCallStop(toolUseID string)` — currently no-op (OpenAI streams don't emit explicit stop per call)
      - `WriteFinalChunk(finishReason string, usage *openai.Usage)` — emits chunk with `finish_reason` populated and `usage` (per OpenAI extensions)
      - `WriteDone()` — emits `data: [DONE]\n\n`
      - `WriteError(err *errs.Error)` — emits `data: {"error":{...}}` then DONE
    - All chunks share `id`, `created`, `model`, `object="chat.completion.chunk"`

  **Must NOT do**: rewrite/parse tool call arguments JSON; emit DONE before the final chunk.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: similar SSE state, indexing
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: T33 | Blocked By: T13

  **References**:
  - Pattern: `kiro-gateway/kiro/streaming_openai.py`
  - External: https://platform.openai.com/docs/api-reference/streaming

  **Acceptance Criteria**:
  - [ ] Tool call streaming: first chunk has full tool metadata; subsequent chunks have only `arguments` delta
  - [ ] Final chunk has `finish_reason` non-null
  - [ ] DONE always emitted last

  **QA Scenarios**:
  ```
  Scenario: Tool call streaming shape
    Tool: Bash
    Steps: go test ./internal/api/openai/... -run TestSSEWriter -v
    Expected: PASS — index 0 first call gets {id, type, function.name}; deltas only have arguments
    Evidence: .sisyphus/evidence/task-23-openai-sse.txt
  ```

  **Commit**: YES | Message: `feat(openai): SSE writer for chat completion chunks (T23)` | Files: internal/api/openai/sse.go, internal/api/openai/sse_test.go

- [x] **T24. Kiro stream decoder (binary frames → unified KiroEvent)**

  **What to do**:
  - Implement `internal/kiro/stream.go`:
    - `type StreamDecoder struct { parser *Parser; toolBuilders map[string]*toolBuilder /* toolUseID → in-progress tool call */; logger *slog.Logger }`
    - Method `Decode(ctx, body io.ReadCloser) <-chan StreamEvent`:
      - Reads chunks from `body` and feeds to `Parser.Feed`
      - For each frame, calls `DecodeEvent` (T11)
      - Translates raw events into unified `StreamEvent` (matches `converter.NormalizedChunk` types):
        - `assistantResponseEvent.content` → `TextDelta`
        - `toolUseEvent`: first event with `Name` non-empty → `ToolUseStart{ID, Name}`; subsequent (with `Stop=false`) → `ToolUseDelta{ID, InputDelta}`; `Stop=true` → `ToolUseStop{ID}`
        - `contextUsageEvent.contextUsagePercentage` → `ContextUsage{Pct}`
        - `meteringEvent` → ignored (no observable behavior)
      - On `ctx.Done()`: close body, emit `Stop{Reason: "client_canceled"}`, close channel
      - On parser error: emit `Error` event then close
      - On clean EOF: emit `Stop{Reason: "end_turn"}` and close
    - Routes the assistant text deltas through `thinking.Parser` (T13) BEFORE emitting, so consumers receive separate thinking/visible deltas
    - Token accounting: estimate output tokens via `tiktoken-go` `cl100k_base` * 1.15 correction factor (matches kiro-gateway), emit final `Usage{InputTokens, OutputTokens}` event before `Stop`. InputTokens estimated from request payload size at start (passed in).
    - Provides helper `Estimator { CountTokens(text string) int }` in `internal/kiro/tokenizer.go` (separate file, but part of T24 scope)

  **Must NOT do**: block the channel send (use buffered channel capacity 64); allow goroutine leak on early-return paths.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: combines streaming + parser + thinking + token accounting
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 4 | Blocks: T29 | Blocked By: T7, T11

  **References**:
  - Pattern: `kiro.rs/src/anthropic/stream.rs` (state machine + thinking detection)
  - Pattern: `kiro-gateway/kiro/streaming_core.py` (parse_kiro_stream)
  - External: https://github.com/pkoukk/tiktoken-go

  **Acceptance Criteria**:
  - [ ] Captured `sample_stream.bin` (T11) decodes to expected sequence of StreamEvents (snapshot)
  - [ ] Cancellation via ctx closes body within 100ms (verified with deadline test)
  - [ ] No goroutine leak (verified with `goleak` package or stdlib equivalent)

  **QA Scenarios**:
  ```
  Scenario: End-to-end stream decode + cancellation
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestStreamDecoder -race -v
    Expected: PASS — decode order matches snapshot; ctx cancel completes < 100ms; no leaks
    Evidence: .sisyphus/evidence/task-24-stream.txt
  ```

  **Commit**: YES | Message: `feat(kiro): stream decoder with thinking + token estimation (T24)` | Files: internal/kiro/stream.go, internal/kiro/tokenizer.go, internal/kiro/stream_test.go

### Wave 5 — Account Orchestration

- [x] **T25. Account manager (selection + ownership)**

  **What to do**:
  - Implement `internal/account/manager.go`:
    - `type Manager struct { store *Store; balancer Balancer; circuit *CircuitBreaker; mu sync.RWMutex; tokenLocks sync.Map /* accountID → *sync.Mutex for refresh DCL */; cfg ManagerConfig; logger *slog.Logger }`
    - `type ManagerConfig { StickySession bool; DefaultRegion string }`
    - `type Acquisition struct { Account *Account; Token string; Region string; ReleaseSuccess func(); ReleaseFailure func(reason string) }`
    - Method `Acquire(ctx context.Context, hint SelectionHint) (*Acquisition, error)`:
      - `SelectionHint { ConversationID string; Model string; ExcludeIDs []string }`
      - Pick candidate via balancer (T26), filtered by:
        - `enabled=true`
        - `circuit.IsOpen(id)==false` OR probabilistic retry triggers (10% chance)
        - Not in `ExcludeIDs`
        - For Opus models: subscription tier check (cached quota.subscriptionTitle must NOT contain "FREE")
      - Acquire per-account refresh lock (`tokenLocks` Map). Inside lock: re-check expiry, refresh if needed via `auth_social.Refresh` or `auth_apikey.Refresh`.
      - Return `Acquisition` with closures `ReleaseSuccess` (calls `store.RecordSuccess`, `circuit.RecordSuccess`) and `ReleaseFailure` (calls `store.RecordFailure`, `circuit.RecordFailure(reason)`).
    - Method `Refresh(ctx, accountID) error` — used by admin endpoint
    - Method `List(ctx) ([]*Account, error)` — pass-through to store with status overlay (`disabled_reason` from circuit if currently in cooldown)
    - Sticky session: if `cfg.StickySession`, the manager remembers `last successful account` globally (NOT per-conversation per kiro-gateway pattern). Stored in-memory only.

  **Must NOT do**: hold the manager mutex across network calls (only for index updates); use a single global mutex for all per-account refresh.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: concurrency-critical path; selection logic; lifecycle ownership
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 5 | Blocks: T26, T29, T36 | Blocked By: T6

  **References**:
  - Pattern: `kiro.rs/src/kiro/token_manager.rs` (CallContext, double-checked locking)
  - Pattern: `kiro-gateway/kiro/account_manager.py` (sticky behavior, exclude_accounts)

  **Acceptance Criteria**:
  - [ ] Concurrent `Acquire` for same account does NOT trigger 2 simultaneous refreshes (verified with httptest counter)
  - [ ] Sticky behavior: after success, next `Acquire` (without exclude) returns same account
  - [ ] Excluded accounts are skipped

  **QA Scenarios**:
  ```
  Scenario: Refresh DCL under concurrent acquire
    Tool: Bash
    Steps: go test ./internal/account/... -run TestManagerRefreshDCL -race -v
    Expected: PASS — exactly 1 refresh call across 50 concurrent Acquire
    Evidence: .sisyphus/evidence/task-25-manager.txt
  ```

  **Commit**: YES | Message: `feat(account): manager with sticky selection and refresh DCL (T25)` | Files: internal/account/manager.go, internal/account/manager_test.go

- [x] **T26. Load balancer (round_robin, balanced, most_quota)**

  **What to do**:
  - Implement `internal/account/balancer.go`:
    - `type Balancer interface { Pick(ctx context.Context, candidates []*Account) (*Account, error) }`
    - Three implementations:
      - `RoundRobin { idx atomic.Uint64 }` — picks `candidates[idx % len]`, increments on success only (caller-driven via Manager)
      - `Balanced` — picks the account with the lowest `success_count` (tiebreak: oldest `last_used_at`)
      - `MostQuota { fetcher *Fetcher }` — picks account with highest cached `LimitRemaining`; if cache empty for an account, treats it as `LimitRemaining=0` and refreshes opportunistically. Never blocks the pick on a fetch (returns immediately with whatever info is cached).
    - Factory `NewBalancer(strategy string, fetcher *Fetcher) (Balancer, error)`: strategy ∈ {`round_robin`, `balanced`, `most_quota`}
    - Manager calls balancer with already-filtered candidates (enabled + non-cooldown + not-excluded)

  **Must NOT do**: pick a disabled or in-cooldown account; mutate state in `Pick` (selection is read-only).

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: well-scoped, three small implementations
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 5 | Blocks: T29 | Blocked By: T17, T25

  **References**:
  - Pattern: `kiro-account-manager/src-tauri/src/gateway/load_balancer.rs` (round_robin, balanced, most_quota strategies)

  **Acceptance Criteria**:
  - [ ] RoundRobin distributes evenly across 3 accounts over 30 picks
  - [ ] Balanced consistently picks lowest-success account
  - [ ] MostQuota returns nil-error even when fetcher cache empty (uses 0 fallback)

  **QA Scenarios**:
  ```
  Scenario: Strategy distribution
    Tool: Bash
    Steps: go test ./internal/account/... -run TestBalancer -v
    Expected: PASS — distribution and ordering correct
    Evidence: .sisyphus/evidence/task-26-balancer.txt
  ```

  **Commit**: YES | Message: `feat(account): load balancer strategies (T26)` | Files: internal/account/balancer.go, internal/account/balancer_test.go

- [x] **T27. Circuit breaker (exponential backoff + probabilistic retry)**

  **What to do**:
  - Implement `internal/account/circuit.go`:
    - `type CircuitBreaker struct { mu sync.RWMutex; states map[string]*state /* accountID → state */; cfg CircuitConfig; clock clockFn }`
    - `type CircuitConfig { BaseCooldown time.Duration; MaxBackoffMultiplier int; ProbabilisticRetryChance float64 }`
    - `type state struct { failures int; lastFailure time.Time; lastReason string }`
    - Methods (all thread-safe):
      - `IsOpen(accountID string) bool` — returns true if `now - lastFailure < BaseCooldown * 2^(failures-1)` capped at `BaseCooldown * MaxBackoffMultiplier`. With probability `ProbabilisticRetryChance` (default 0.10), returns false even when in cooldown (probabilistic retry of broken accounts).
      - `RecordSuccess(accountID string)` — resets `failures=0`
      - `RecordFailure(accountID string, reason string)` — increments `failures`, updates `lastFailure`
      - `Reason(accountID string) string` — returns last failure reason if currently open
      - `Snapshot() map[string]CircuitInfo` — for admin/list reporting
    - Persistence: state is in-memory only (failure_count IS persisted in DB via T6, but cooldown timing doesn't survive restart — that's intentional, matching kiro-gateway behavior). On startup, load `failure_count` from DB into in-memory state, `lastFailure=now` (so cooldown re-applies briefly post-restart).

  **Must NOT do**: persist cooldown timestamps; use `math/rand` without seeding (`crypto/rand` for probabilistic retry).

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: well-defined formula + simple state
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 5 | Blocks: T29 | Blocked By: T25

  **References**:
  - Pattern: `kiro-gateway/kiro/account_manager.py` (BASE_COOLDOWN * 2^failures, probabilistic retry)

  **Acceptance Criteria**:
  - [ ] After 1 failure: cooldown ≈ BASE; after 2: ≈ 2*BASE; after 5: ≈ 16*BASE; after 11: capped at MAX_BACKOFF*BASE
  - [ ] Probabilistic retry: over 1000 calls during cooldown with chance=0.5, returns false ~500 times (±10%)
  - [ ] Concurrent `IsOpen`+`RecordFailure` from 100 goroutines doesn't race (`-race` clean)

  **QA Scenarios**:
  ```
  Scenario: Cooldown formula + probabilistic retry distribution
    Tool: Bash
    Steps: go test ./internal/account/... -run TestCircuit -race -v
    Expected: PASS
    Evidence: .sisyphus/evidence/task-27-circuit.txt
  ```

  **Commit**: YES | Message: `feat(account): circuit breaker with exponential backoff (T27)` | Files: internal/account/circuit.go, internal/account/circuit_test.go

- [x] **T28. JSON file watcher (fsnotify)**

  **What to do**:
  - Implement `internal/account/watcher.go`:
    - `type Watcher struct { path string; store *Store; logger *slog.Logger; debounce time.Duration }`
    - File schema (declarative array):
      ```json
      [
        {"id":"opt-uuid", "label":"acct1", "auth_method":"social", "refresh_token":"...", "profile_arn":"...", "region":"us-east-1", "proxy_url":"http://...", "enabled":true},
        {"id":"opt-uuid", "label":"acct2", "auth_method":"apikey", "api_key":"ksk_...", "enabled":true}
      ]
      ```
    - Method `Run(ctx context.Context) error`:
      - Initial sync: parse file, reconcile to DB:
        - For each entry with `id`: upsert (insert or update existing row; preserve `success_count`, `failure_count`, `access_token`, `expires_at` if not in JSON)
        - For each entry without `id`: lookup by `(auth_method, refresh_token)` or `(auth_method, api_key)`; if found update, else insert with new uuid
        - Entries with `"_delete": true` and `id` set are deleted from DB
        - Entries NOT in file: kept untouched (DB is source-of-truth for accounts added via CLI/REST). Optional: `"_remove_unlisted": true` top-level flag triggers removal of file-managed accounts not present.
        - File MUST be a JSON array; non-array rejected with WARN log + no changes
      - Watch loop: fsnotify on parent directory (handles editor save patterns); debounce events by `debounce` (default 500ms); call sync on each debounced trigger
    - Generates `machine_id` for each new account via T12 with seed = label or uuid
  - Add unit test using temp dir + fsnotify

  **Must NOT do**: read file on every change without debouncing; partially apply on parse error (atomic-ish: parse first, then transactional reconcile).

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: edge cases on filesystem events + reconciliation semantics
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 5 | Blocks: T38 | Blocked By: T6

  **References**:
  - External: https://github.com/fsnotify/fsnotify
  - Pattern: `kiro-gateway/kiro/account_manager.py` (declarative credentials.json reload)

  **Acceptance Criteria**:
  - [ ] Initial sync inserts all rows
  - [ ] Edit file → debounced reconcile within 1s
  - [ ] Malformed JSON: WARN logged, DB unchanged
  - [ ] Removing entry with `"_remove_unlisted": true` deletes only that file-managed account

  **QA Scenarios**:
  ```
  Scenario: Debounced reload
    Tool: Bash
    Steps: go test ./internal/account/... -run TestWatcher -v
    Expected: PASS — multiple writes within 500ms collapse to single sync
    Evidence: .sisyphus/evidence/task-28-watcher.txt
  ```

  **Commit**: YES | Message: `feat(account): fsnotify-based credentials watcher (T28)` | Files: internal/account/watcher.go, internal/account/watcher_test.go

- [x] **T29. Kiro client high-level (request_with_failover)**

  **What to do**:
  - Implement `internal/kiro/dispatcher.go`:
    - `type Dispatcher struct { client *Client; manager *account.Manager; cfg DispatcherConfig; logger *slog.Logger }`
    - `type DispatcherConfig { MaxAttempts int /* 9 */; BaseRetryMs int /* 100 */; }`
    - Method `Stream(ctx, payload *KiroPayload, hint account.SelectionHint) (<-chan StreamEvent, error)`:
      - Outer failover loop: up to `cfg.MaxAttempts`
      - Per attempt:
        1. `acq, err := manager.Acquire(ctx, hint)`; if `err==NoAccountsAvailable` return immediately
        2. `req, err := buildKiroRequest(acq.Account, payload, acq.Token, acq.Region)` — POST to `https://q.{apiRegion}.amazonaws.com/generateAssistantResponse`, body=JSON(payload), headers from T16
        3. `body, resp, err := client.Stream(ctx, acq.Account, req)`
        4. If err → classify via T5 → if RECOVERABLE: `acq.ReleaseFailure(reason)`; add account.ID to hint.ExcludeIDs; continue. If FATAL: `acq.ReleaseFailure`; return err.
        5. If `resp.StatusCode != 200` → classify via `errs.FromKiroResponse`. Same recovery decision. For 401/403, force-refresh token once before failing-over (caller still excludes after refresh failure).
        6. Otherwise: SUCCESS path. Spawn goroutine to drive `StreamDecoder.Decode(ctx, body)`, forward events to caller channel. On stream completion, call `acq.ReleaseSuccess()`. On mid-stream error, call `acq.ReleaseFailure("mid_stream_error")` — do NOT failover (already streaming to client).
      - Backoff between attempts: `BaseRetryMs * 2^attempt` capped at 2s + jitter
      - All attempts exhausted → return `errs.New(ClassFatal, "ALL_ACCOUNTS_FAILED", ...)` with last-known cause
    - Method `Once(ctx, payload, hint) (FullResponse, error)` for non-streaming: collects all StreamEvents into a single response (text + tool_use + final usage). Used by `/v1/messages` and `/v1/chat/completions` non-streaming paths.

  **Must NOT do**: send `<thinking>` injection here (already in T20); start failover after first byte streamed (clients expect monotonic stream); leak the channel goroutine on early-return.

  **Recommended Agent Profile**:
  - Category: `deep` — Reason: orchestration of all prior components, error paths are extensive
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 5 | Blocks: T31, T33 | Blocked By: T5, T14, T15, T20, T21, T24, T25, T26, T27

  **References**:
  - Pattern: `kiro.rs/src/kiro/provider.rs` (call_api_with_retry — 3 per-account, 9 total)
  - Pattern: `kiro-gateway/kiro/routes_anthropic.py` failover loop

  **Acceptance Criteria**:
  - [ ] 3 accounts, first 2 return 402: third succeeds; client sees only third's stream
  - [ ] 401 on first account: token force-refreshed once, then if still fails, account excluded and next tried
  - [ ] Mid-stream cancel via ctx: goroutine exits within 100ms, no leak

  **QA Scenarios**:
  ```
  Scenario: Multi-account failover
    Tool: Bash
    Steps: go test ./internal/kiro/... -run TestDispatcherFailover -race -v
    Expected: PASS — 402 → 402 → 200 path verified; client receives only third stream
    Evidence: .sisyphus/evidence/task-29-dispatcher.txt
  ```

  **Commit**: YES | Message: `feat(kiro): dispatcher with multi-account failover (T29)` | Files: internal/kiro/dispatcher.go, internal/kiro/dispatcher_test.go

### Wave 6 — HTTP Surface

- [x] **T30. Gin server skeleton + middleware**

  **What to do**:
  - Implement `internal/server/server.go`:
    - `type Server struct { engine *gin.Engine; cfg *config.Config; logger *slog.Logger; manager *account.Manager; dispatcher *kiro.Dispatcher; quotaFetcher *account.Fetcher }`
    - `New(deps Deps) *Server` builds the engine with `gin.New()` (NOT `gin.Default()`), attaches middleware in this order:
      1. `RequestIDMiddleware` (T3): generates uuid v4 if not present in `X-Request-ID`, stores in context
      2. `LoggingMiddleware` (T3): logs after handler with redaction
      3. `RecoverMiddleware`: catches panics, returns 500 JSON `{"error":{"type":"internal_error","message":"..."}}`, logs stack trace
      4. `CORSMiddleware`: allows `Authorization`, `Content-Type`, `x-api-key`, `anthropic-version` headers; allow `*` origin (callers are local agents typically)
      5. `ProxyAuthMiddleware`: applied to `/v1/*` group only. Verifies `Authorization: Bearer <ProxyAPIKey>` OR `x-api-key: <ProxyAPIKey>`. Returns 401 JSON on mismatch.
      6. `AdminAuthMiddleware`: applied to `/admin/*` group only. Verifies `Authorization: Bearer <AdminAPIKey>`. Returns 401 JSON on mismatch.
    - `Run(ctx context.Context) error`: starts `http.Server` with `Handler: s.engine`, `ReadHeaderTimeout: 10s`, `IdleTimeout: 60s`, NO write/read timeout (streaming); on `ctx.Done()`, calls `Shutdown(ctx, 30s)` for graceful drain
  - Routes registered in this task (handlers stubbed/wired in T31-T36):
    - `GET /health` → 200 `{"status":"ok"}` (NO auth)
    - `/v1/*` group with proxy auth
    - `/admin/*` group with admin auth

  **Must NOT do**: use `gin.Default()` (it adds its own logger that conflicts); leave a route without auth that should have it; emit panic stack to client.

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: standard gin wiring
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: T31-T36 | Blocked By: T2, T3

  **References**:
  - External: https://gin-gonic.com/docs/examples/custom-middleware/
  - Pattern: `kiro-account-manager/src-tauri/src/gateway/mod.rs` (auth + IP allowlist middleware)

  **Acceptance Criteria**:
  - [ ] `GET /health` works without auth, returns `{"status":"ok"}`
  - [ ] `POST /v1/messages` without `Authorization` returns 401 JSON
  - [ ] `GET /admin/accounts` without admin key returns 401 JSON
  - [ ] Wrong key returns 401 with `Bearer realm` consistent error shape
  - [ ] Recover middleware: forced panic in test handler returns 500, no process crash

  **QA Scenarios**:
  ```
  Scenario: Auth enforcement matrix
    Tool: Bash
    Steps: go test ./internal/server/... -run TestAuth -v
    Expected: PASS — health open, /v1 needs proxy key, /admin needs admin key
    Evidence: .sisyphus/evidence/task-30-server.txt
  ```

  **Commit**: YES | Message: `feat(server): gin engine with auth and recovery middleware (T30)` | Files: internal/server/server.go, internal/server/middleware/*.go, internal/server/server_test.go

- [x] **T31. POST /v1/messages handler (Anthropic, stream + non-stream)**

  **What to do**:
  - Implement `internal/api/anthropic/handler.go`:
    - `type Handler struct { dispatcher *kiro.Dispatcher; tokenizer *kiro.Estimator; logger *slog.Logger }`
    - Route: `POST /v1/messages`
    - Body parse: `MessagesRequest` (T8); on error 400 with shape `{"error":{"type":"invalid_request_error","message":"..."}}`
    - Validate: T8.Validate
    - Convert: `AnthropicToNormalized` (T18) → `NormalizedToKiro` (T20)
    - Determine streaming via `req.Stream`
    - Streaming path:
      1. Set headers `Content-Type: text/event-stream`, `Cache-Control: no-cache`, `Connection: keep-alive`, `X-Accel-Buffering: no`
      2. Flush headers
      3. Create `SSEWriter` (T22) with messageID = `msg_<uuid>` and model = req.Model
      4. Estimate input_tokens, write `message_start`
      5. Open `dispatcher.Stream(ctx, payload, hint)`; consume events, route to SSE writer:
         - First TextDelta: open content_block_start(index=N, type="text"); subsequent: content_block_delta
         - First ThinkingDelta: open content_block_start(0, "thinking"); subsequent: delta
         - ToolUseStart: open content_block_start(index, "tool_use", id, name); ToolUseDelta → input_json_delta; ToolUseStop → content_block_stop
         - On Stop event: stop all open blocks, write message_delta + message_stop
         - Periodic ping every 25s while no event (separate goroutine, halt on completion)
      6. Map dispatcher errors to SSE error event then close stream
    - Non-streaming path:
      1. `dispatcher.Once(ctx, payload, hint)`
      2. Build `MessagesResponse` from collected events: text → ContentBlock{type:text}, tool_use → ContentBlock{type:tool_use}, thinking → ContentBlock{type:thinking}
      3. Set `stop_reason` (`end_turn` | `tool_use` | `max_tokens` from final stop)
      4. Set `usage.input_tokens` (estimated) and `usage.output_tokens` (from dispatcher final usage)
      5. Return JSON 200
    - Token counting endpoint stub: registered separately in T32

  **Must NOT do**: forget to flush after each SSE write; leave goroutine leak on client disconnect; emit DONE for Anthropic (DONE is OpenAI only).

  **Recommended Agent Profile**:
  - Category: `unspecified-high` — Reason: handler orchestrates many pieces; correctness across stream + non-stream
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: T38 | Blocked By: T18, T22, T29, T30

  **References**:
  - Pattern: `kiro-gateway/kiro/routes_anthropic.py`
  - Pattern: `kiro.rs/src/anthropic/handlers.rs`

  **Acceptance Criteria**:
  - [ ] Streaming: against a mock Kiro server fixture, returns valid SSE matching expected sequence
  - [ ] Non-streaming: returns valid `MessagesResponse` JSON with content blocks
  - [ ] Client disconnect mid-stream: dispatcher goroutine exits within 100ms

  **QA Scenarios**:
  ```
  Scenario: Streaming + non-streaming end-to-end
    Tool: Bash
    Steps: go test ./internal/api/anthropic/... -run TestHandlerE2E -race -v
    Expected: PASS — both modes produce expected output
    Evidence: .sisyphus/evidence/task-31-anthropic-handler.txt

  Scenario: Mid-stream client cancel
    Tool: Bash
    Steps: go test ./internal/api/anthropic/... -run TestHandlerCancel -race -v
    Expected: PASS — no goroutine leak, dispatcher cleanup observed
    Evidence: .sisyphus/evidence/task-31-anthropic-cancel.txt
  ```

  **Commit**: YES | Message: `feat(anthropic): /v1/messages handler with stream + non-stream (T31)` | Files: internal/api/anthropic/handler.go, internal/api/anthropic/handler_test.go

- [x] **T32. POST /v1/messages/count_tokens**

  **What to do**:
  - Implement in `internal/api/anthropic/handler.go` (same handler struct):
    - Route: `POST /v1/messages/count_tokens`
    - Body parse: `CountTokensRequest` (T8)
    - Convert messages to plain text (concat text content + tool descriptions + system prompt) and call `kiro.Estimator.CountTokens`
    - Return `{"input_tokens": N}` JSON 200

  **Must NOT do**: hit Kiro upstream (this is a local estimation); over-engineer for tool input schemas (just count their JSON-encoded text).

  **Recommended Agent Profile**:
  - Category: `quick` — Reason: trivial wrapper around Estimator
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: T38 | Blocked By: T8, T30

  **References**:
  - External: https://docs.anthropic.com/en/api/messages-count-tokens

  **Acceptance Criteria**:
  - [ ] Returns `{"input_tokens": N}` for a fixture; N within ±5% of tiktoken*1.15 baseline

  **QA Scenarios**:
  ```
  Scenario: Token count for fixture
    Tool: Bash
    Steps: go test ./internal/api/anthropic/... -run TestCountTokens -v
    Expected: PASS
    Evidence: .sisyphus/evidence/task-32-count-tokens.txt
  ```

  **Commit**: YES | Message: `feat(anthropic): /v1/messages/count_tokens endpoint (T32)` | Files: internal/api/anthropic/handler.go (additions), internal/api/anthropic/handler_test.go (additions)

- [x] **T33. POST /v1/chat/completions handler (OpenAI, stream + non-stream)**

  **What to do**:
  - Implement `internal/api/openai/handler.go`:
    - Mirror T31 structure but for OpenAI
    - Route: `POST /v1/chat/completions`
    - Body parse: `ChatCompletionRequest` (T9), validate
    - Convert: `OpenAIToNormalized` (T19) → `NormalizedToKiro` (T20)
    - Streaming path:
      1. Headers same as T31
      2. `SSEWriter` (T23) with `id="chatcmpl-<uuid>"`, `created=now-unix`
      3. Optional first chunk with `delta.role="assistant"` (matches OpenAI behavior)
      4. Consume dispatcher events:
         - TextDelta → WriteContentDelta
         - ThinkingDelta → WriteReasoningDelta (only if `req.ReasoningEffort != ""` OR thinking budget > 0; otherwise drop thinking)
         - ToolUseStart → WriteToolCallStart; Delta → WriteToolCallArgsDelta; Stop → no-op
         - Stop → WriteFinalChunk(finishReason mapped: end_turn→stop, tool_use→tool_calls, max_tokens→length)
         - Final: WriteDone
    - Non-streaming path:
      1. `dispatcher.Once`
      2. Build `ChatCompletionResponse`: assistant message with `content` and `tool_calls`; thinking goes into `reasoning_content` (OpenAI extension) only if requested
      3. Return JSON 200

  **Must NOT do**: skip the `[DONE]` sentinel; merge two distinct tool_calls into one chunk by mistake.

  **Recommended Agent Profile**:
  - Category: `unspecified-high` — Reason: handler orchestration
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: T38 | Blocked By: T19, T23, T29, T30

  **References**:
  - Pattern: `kiro-gateway/kiro/routes_openai.py`
  - Pattern: `kiro-gateway/kiro/streaming_openai.py`

  **Acceptance Criteria**:
  - [ ] Streaming: against mock Kiro fixture, returns valid OpenAI SSE
  - [ ] Non-streaming: valid `ChatCompletionResponse` JSON
  - [ ] Tool call streaming: index correctness across multiple tool calls

  **QA Scenarios**:
  ```
  Scenario: Streaming with tool calls
    Tool: Bash
    Steps: go test ./internal/api/openai/... -run TestHandlerStreamTools -race -v
    Expected: PASS — multiple tool_calls indexed correctly
    Evidence: .sisyphus/evidence/task-33-openai-handler.txt
  ```

  **Commit**: YES | Message: `feat(openai): /v1/chat/completions handler with stream + non-stream (T33)` | Files: internal/api/openai/handler.go, internal/api/openai/handler_test.go

- [x] **T34. GET /v1/models**

  **What to do**:
  - Implement in `internal/api/openai/handler.go` (or split to `models.go`):
    - Route: `GET /v1/models`
    - Returns hardcoded list of supported Kiro models in OpenAI ModelsResponse shape:
      - `claude-sonnet-4.5`, `claude-sonnet-4.6`, `claude-opus-4.5`, `claude-opus-4.6`, `claude-opus-4.7`, `claude-haiku-4.5`
      - Each: `{id, object:"model", created: <static>, owned_by: "kiro"}`
    - **Optional enhancement** (only if implemented elsewhere; otherwise omit): query `account.Manager.Manager` to filter by which models any enabled account has access to via cached quota. For MVP: return all 6.

  **Must NOT do**: hit Kiro `ListAvailableModels` (probe pattern violation); allow unauthenticated access (still under `/v1/*` so proxy auth applies).

  **Recommended Agent Profile**:
  - Category: `quick` — Reason: hardcoded list
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: T38 | Blocked By: T9, T17, T30

  **References**:
  - External: https://platform.openai.com/docs/api-reference/models/list

  **Acceptance Criteria**:
  - [ ] Returns 6 models in expected shape
  - [ ] Auth required (under /v1/*)

  **QA Scenarios**:
  ```
  Scenario: Models endpoint
    Tool: Bash
    Steps: go test ./internal/api/openai/... -run TestModels -v
    Expected: PASS
    Evidence: .sisyphus/evidence/task-34-models.txt
  ```

  **Commit**: YES | Message: `feat(openai): /v1/models endpoint (T34)` | Files: internal/api/openai/models.go, internal/api/openai/models_test.go

- [x] **T35. GET /health**

  **What to do**:
  - Already wired in T30; add a small handler that returns `{"status":"ok","version":"<git-sha or build-time-injected>"}`
  - Inject build version via `-ldflags "-X main.Version=$(git rev-parse --short HEAD)"`
  - Move version constant into `internal/version/version.go`

  **Must NOT do**: include account counts or DB stats (information leak; admin endpoint owns that).

  **Recommended Agent Profile**:
  - Category: `quick` — Reason: trivial
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: T38 | Blocked By: T30

  **Acceptance Criteria**:
  - [ ] Returns 200 JSON with status:ok
  - [ ] Includes build version when set via ldflags

  **QA Scenarios**:
  ```
  Scenario: Health endpoint smoke
    Tool: Bash
    Steps: go test ./internal/server/... -run TestHealth -v
    Expected: PASS
    Evidence: .sisyphus/evidence/task-35-health.txt
  ```

  **Commit**: YES | Message: `feat(server): health endpoint with build version (T35)` | Files: internal/version/version.go, internal/server/server.go (additions)

- [x] **T36. Admin /admin/accounts CRUD + /admin/quota**

  **What to do**:
  - Implement `internal/api/admin/handler.go`:
    - All routes under `/admin/*` with admin auth (T30)
    - `POST /admin/accounts`:
      - Body: `{ "label": "...", "auth_method": "social"|"apikey", "refresh_token": "...", "api_key": "...", "profile_arn": "...", "region": "...", "auth_region": "...", "api_region": "...", "proxy_url": "...", "proxy_username": "...", "proxy_password": "...", "enabled": true }`
      - Validate: auth_method ∈ {social, apikey}; for social → refresh_token required; for apikey → api_key required (must start `ksk_`)
      - Generate `id` (uuid v4) and `machine_id` (T12, seed = label or id)
      - Insert via `store.Create`
      - For social: trigger an immediate refresh (T14) to populate `access_token` + `expires_at`. If refresh fails: leave the account but set `enabled=false` with disabled_reason
      - Return 201 with the created Account (redact `refresh_token`, `api_key`, `proxy_password` in response)
    - `GET /admin/accounts`:
      - Query params: `?enabled_only=true|false`, `?auth_method=...`
      - Returns array of Accounts (redacted)
    - `GET /admin/accounts/:id`:
      - Returns single account (redacted) + circuit breaker status (open/closed, failures, last_reason)
    - `PATCH /admin/accounts/:id`:
      - Body: any subset of `{label, enabled, proxy_url, proxy_username, proxy_password, region, auth_region, api_region}`
      - Validates and updates via `store.Update`
    - `DELETE /admin/accounts/:id`:
      - Removes account; cascades to `quota_cache` via FK
    - `POST /admin/accounts/:id/refresh`:
      - Forces token refresh via `manager.Refresh`
      - Returns updated account (redacted) or error
    - `GET /admin/accounts/:id/quota`:
      - Query param `?force=true` to bypass cache
      - Returns `Quota` shape from T17
    - `GET /admin/quota`:
      - Returns array `[{account_id, label, subscription_title, limit_total, limit_remaining, fetched_at, stale}]`
      - `stale=true` if cache age > TTL
      - Does NOT auto-refresh (probe avoidance)

  **Must NOT do**: return raw secrets in responses; allow PATCH on auth_method (immutable post-create); auto-refresh quota on `GET /admin/quota` (probe avoidance).

  **Recommended Agent Profile**:
  - Category: `unspecified-high` — Reason: many endpoints, redaction logic, validation
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 6 | Blocks: T38 | Blocked By: T6, T17, T25, T30

  **References**:
  - Pattern: `kiro-account-manager/src-tauri/src/commands/account_cmd.rs` (CRUD shape)

  **Acceptance Criteria**:
  - [ ] POST/GET/PATCH/DELETE all work with table-driven tests
  - [ ] Secret redaction verified: refresh_token field is `[REDACTED]` (or absent) in all responses
  - [ ] Force quota refresh bypasses cache; without force, second call within TTL serves cache

  **QA Scenarios**:
  ```
  Scenario: Full CRUD + redaction
    Tool: Bash
    Steps: go test ./internal/api/admin/... -run TestAdminFull -v
    Expected: PASS — all endpoints verified, no secret leak
    Evidence: .sisyphus/evidence/task-36-admin.txt
  ```

  **Commit**: YES | Message: `feat(admin): account CRUD and quota endpoints (T36)` | Files: internal/api/admin/handler.go, internal/api/admin/handler_test.go

### Wave 7 — CLI + Wire-up

- [x] **T37. cobra CLI commands**

  **What to do**:
  - Implement `cmd/cli/main.go` and `internal/cli/*.go` using `spf13/cobra`:
    - Root: `kiro-let-go-cli` with persistent flags `--config <path>` (default `configs/config.json`), `--db <path>` (overrides config storage path), `--json` (output JSON instead of table)
    - Sub-commands:
      - `account add` flags: `--type=social|apikey`, `--label`, `--refresh-token`, `--api-key`, `--profile-arn`, `--region`, `--auth-region`, `--api-region`, `--proxy-url`, `--proxy-username`, `--proxy-password`. Generates `machine_id` (T12), inserts via store, for social runs immediate refresh.
      - `account list` flags: `--enabled-only`, `--auth-method=...`. Tabular output by default.
      - `account get <id>` — single account with circuit breaker status.
      - `account remove <id>` — confirmation prompt unless `--yes`.
      - `account enable <id>` / `account disable <id> --reason "..."`.
      - `account refresh <id>` — force token refresh (calls auth providers directly, NOT the running server).
      - `quota` (no arg → all accounts; `<id>` → single) — flag `--force` for bypass cache (will hit Kiro API once).
      - `server` — alias for `cmd/server/main.go` invocation, exits with the same codes (this is for convenience; primary entry point remains `cmd/server`).
    - Error handling: print to stderr, exit 1 on failure, 0 on success.
    - `--json` output: machine-readable for scripting.
  - Each command:
    - Loads config via T2
    - Opens SQLite via T4 (no migration here — server takes care of it; CLI errors if DB doesn't exist with helpful message: "Run server once to initialize, or use --db pointing to existing DB")
    - Calls into account/store directly (does NOT go through HTTP — CLI is filesystem-coupled, useful when server is offline)

  **Must NOT do**: hit the running server's REST API (CLI is independent); print secrets to stdout (redact like admin API); make `account add` write to JSON file (DB is canonical; file watch is the OTHER channel).

  **Recommended Agent Profile**:
  - Category: `unspecified-low` — Reason: cobra wiring is mechanical; many small commands
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 7 | Blocks: T39 | Blocked By: T6, T14, T15, T17

  **References**:
  - External: https://github.com/spf13/cobra
  - Pattern: `kiro-account-manager/src-tauri/src/commands/account_cmd.rs` (operation list)

  **Acceptance Criteria**:
  - [ ] `account add --type=apikey --key=ksk_test_dummy --label=t1` writes one row to SQLite
  - [ ] `account list` shows the row in tabular form
  - [ ] `account list --json` outputs valid JSON array
  - [ ] `account remove <id>` without `--yes` prompts; with `--yes` deletes immediately
  - [ ] All commands exit 0 on success, 1 on error

  **QA Scenarios**:
  ```
  Scenario: Full CLI workflow
    Tool: interactive_bash
    Steps:
      ./bin/kiro-let-go-cli account add --type=apikey --key=ksk_qa_test --label=qa1 --json
      ./bin/kiro-let-go-cli account list --json
      ID=$(./bin/kiro-let-go-cli account list --json | jq -r '.[0].id')
      ./bin/kiro-let-go-cli account get "$ID" --json
      ./bin/kiro-let-go-cli account disable "$ID" --reason "qa-test"
      ./bin/kiro-let-go-cli account enable "$ID"
      ./bin/kiro-let-go-cli account remove "$ID" --yes
    Expected: Each command succeeds; JSON parseable at each step; final list shows 0 accounts
    Evidence: .sisyphus/evidence/task-37-cli.txt

  Scenario: Quota command without server running
    Tool: interactive_bash
    Steps: ./bin/kiro-let-go-cli quota --json
    Expected: Returns cached quota for all accounts (or empty list if none); does NOT crash if no quota cached
    Evidence: .sisyphus/evidence/task-37-cli-quota.txt
  ```

  **Commit**: YES | Message: `feat(cli): cobra-based account management commands (T37)` | Files: cmd/cli/main.go, internal/cli/**

- [x] **T38. cmd/server/main.go wire-up + graceful shutdown**

  **What to do**:
  - Implement `cmd/server/main.go`:
    - Parse CLI flags (mirror config layer): `--config`, `--server.host`, `--server.port`, `--storage.sqlite-path`, etc.
    - Load config (T2), build logger (T3)
    - Open SQLite (T4), apply migrations
    - Construct dependency tree (constructor injection):
      - `store := account.NewStore(db)`
      - `circuit := account.NewCircuitBreaker(cfg.Failover, ...)` and load failure_count from DB
      - `socialAuth := kiro.NewSocialAuth(...)`
      - `apikeyAuth := kiro.NewAPIKeyAuth()`
      - `client := kiro.NewClient(...)`
      - `quotaFetcher := account.NewFetcher(client, store, cfg.Quota.CacheTTL, ...)`
      - `balancer := account.NewBalancer(cfg.LoadBalancer.Strategy, quotaFetcher)`
      - `manager := account.NewManager(store, balancer, circuit, socialAuth, apikeyAuth, cfg.LoadBalancer.StickySession, ...)`
      - `dispatcher := kiro.NewDispatcher(client, manager, cfg.Failover, logger)`
      - `srv := server.New(server.Deps{Cfg, Logger, Manager, Dispatcher, QuotaFetcher})`
    - File watcher (T28): start only if `cfg.Storage.CredentialsJSONPath != ""`. Run in errgroup.
    - Top-level `errgroup`:
      - Server `Run`
      - File watcher `Run` (if enabled)
      - Signal handler: SIGINT/SIGTERM → cancel root ctx
    - Graceful shutdown:
      - On signal, close listener (stops new connections)
      - Wait up to 30s for in-flight handlers (uses `http.Server.Shutdown`)
      - Streaming requests honor ctx cancellation (already wired in T29)
      - Close DB last
    - Exit codes: 0 normal, 1 fatal startup error, 2 unclean shutdown (drain timeout)

  **Must NOT do**: panic on signal; close DB before server drain; ignore file watcher errors silently (log WARN at minimum).

  **Recommended Agent Profile**:
  - Category: `unspecified-high` — Reason: assembly + lifecycle ordering matters
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 7 | Blocks: F1-F4 | Blocked By: T2, T28, T30, T31, T32, T33, T34, T35, T36

  **References**:
  - Pattern: `kiro-gateway/main.py` lifespan
  - External: https://pkg.go.dev/golang.org/x/sync/errgroup

  **Acceptance Criteria**:
  - [ ] `./bin/kiro-let-go server --config configs/config.example.json` starts and binds the configured port
  - [ ] SIGTERM → exits within 30s with status 0
  - [ ] In-flight streaming request: completes or cancels gracefully before exit (verified by integration test that opens a stream then signals)
  - [ ] Failed startup (e.g., port in use): exits 1 with clear error log

  **QA Scenarios**:
  ```
  Scenario: Lifecycle smoke
    Tool: interactive_bash
    Steps:
      ./bin/kiro-let-go server --config configs/config.example.json &
      SERVER_PID=$!
      sleep 1
      curl -sf http://127.0.0.1:8765/health
      kill -TERM $SERVER_PID
      wait $SERVER_PID; echo "exit=$?"
    Expected: health returns 200; SIGTERM produces exit 0 within 30s
    Evidence: .sisyphus/evidence/task-38-lifecycle.txt

  Scenario: In-flight stream shutdown
    Tool: Bash
    Steps: go test ./cmd/server/... -run TestGracefulShutdown -race -v
    Expected: PASS — stream completes or cancels cleanly during shutdown
    Evidence: .sisyphus/evidence/task-38-shutdown.txt
  ```

  **Commit**: YES | Message: `feat(server): main entry with graceful shutdown and watcher (T38)` | Files: cmd/server/main.go

- [x] **T39. cmd/cli/main.go wire-up**

  **What to do**:
  - Implement `cmd/cli/main.go` thin shim:
    - Construct cobra root from T37 packages
    - On error: print to stderr with red prefix `[error]`; exit 1
    - On success: exit 0
  - Adds `version` subcommand printing same build version as `/health` (T35)
  - Validates no command conflicts with cobra reserved names

  **Must NOT do**: duplicate logic from `internal/cli`; embed config defaults different from server.

  **Recommended Agent Profile**:
  - Category: `quick` — Reason: trivial wrapper
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 7 | Blocks: F1-F4 | Blocked By: T37

  **Acceptance Criteria**:
  - [ ] `./bin/kiro-let-go-cli version` prints version and exits 0
  - [ ] `./bin/kiro-let-go-cli unknown` returns non-zero with helpful message

  **QA Scenarios**:
  ```
  Scenario: CLI entry smoke
    Tool: Bash
    Steps: ./bin/kiro-let-go-cli version && ./bin/kiro-let-go-cli --help
    Expected: Both exit 0
    Evidence: .sisyphus/evidence/task-39-cli-entry.txt
  ```

  **Commit**: YES | Message: `feat(cli): main entry with version subcommand (T39)` | Files: cmd/cli/main.go

- [x] **T40. Configuration examples + README**

  **What to do**:
  - Create `configs/config.example.json` with all fields from T2 documented inline (using `// json:hint` style or a sibling `config.example.md`):
    ```json
    {
      "server": { "host": "0.0.0.0", "port": 8765, "admin_api_key": "REPLACE_ME_ADMIN", "proxy_api_key": "REPLACE_ME_PROXY" },
      "kiro": { "region": "us-east-1", "auth_region": "us-east-1", "api_region": "us-east-1" },
      "storage": { "sqlite_path": ".data/kiro.db", "credentials_json_path": "" },
      "load_balancer": { "strategy": "round_robin", "sticky_session": true },
      "quota": { "cache_ttl_seconds": 43200 },
      "failover": { "base_cooldown_sec": 60, "max_backoff_multiplier": 1440, "probabilistic_retry_chance": 0.10, "max_attempts": 9 },
      "logging": { "level": "info", "format": "json" }
    }
    ```
  - Create `configs/credentials.example.json` (file-watcher format from T28):
    ```json
    [
      {"label":"social-acct","auth_method":"social","refresh_token":"<your-kiro-refresh-token>","profile_arn":"<your-arn>","region":"us-east-1","enabled":true},
      {"label":"apikey-acct","auth_method":"apikey","api_key":"ksk_xxxxxxxxxxxxxxxxxxxxxx","enabled":true}
    ]
    ```
  - `README.md` sections (concise, operational):
    1. **What is this?** One-paragraph: gateway in front of Kiro accounts, OpenAI/Anthropic compat.
    2. **Build**: `make build`
    3. **Configure**: copy `configs/config.example.json` → `configs/config.json`, fill `admin_api_key` and `proxy_api_key`.
    4. **Add an account** (3 ways):
       - CLI: `./bin/kiro-let-go-cli account add --type=social --refresh-token=...`
       - REST: `curl -X POST -H "Authorization: Bearer $ADMIN_KEY" -d '...' http://localhost:8765/admin/accounts`
       - File: edit `credentials.json`, server reloads automatically
    5. **Use it**: example `curl` for `/v1/messages` (Anthropic) and `/v1/chat/completions` (OpenAI).
    6. **Check quota**: `./bin/kiro-let-go-cli quota` or `GET /admin/quota`.
    7. **Anti-ban notes**: brief note on machine_id, header stability, proxy use.
    8. **Configuration reference**: pointer to `configs/config.example.json`.
    9. **Limitations**: what's NOT supported (IdC, Web UI, response cache, MCP/web search).

  **Must NOT do**: embed real credentials or API keys in examples; use marketing tone; promise features outside scope.

  **Recommended Agent Profile**:
  - Category: `writing` — Reason: documentation
  - Skills: none

  **Parallelization**: Can Parallel: YES | Wave 7 | Blocks: F1-F4 | Blocked By: T38, T39

  **Acceptance Criteria**:
  - [ ] Both example config files parse without error (validated by config loader test)
  - [ ] README has all 9 sections
  - [ ] All shell commands in README copy-paste-runnable

  **QA Scenarios**:
  ```
  Scenario: README command verification
    Tool: Bash
    Steps: bash -n <(grep -E '^\$ ' README.md | sed 's/^\$ //')  # syntax check copied commands
    Expected: No syntax errors
    Evidence: .sisyphus/evidence/task-40-readme.txt

  Scenario: Example config loads cleanly
    Tool: Bash
    Steps: go test ./internal/config/... -run TestExampleConfigLoads -v
    Expected: PASS — example file parses without validation error (after replacing REPLACE_ME values)
    Evidence: .sisyphus/evidence/task-40-config-example.txt
  ```

  **Commit**: YES | Message: `docs: configuration examples and README (T40)` | Files: configs/config.example.json, configs/credentials.example.json, README.md


## Final Verification Wave (MANDATORY — after ALL implementation tasks)
> 4 review agents run in PARALLEL. ALL must APPROVE. Present consolidated results to user and get explicit "okay" before completing.
> **Do NOT auto-proceed after verification. Wait for user's explicit approval before marking work complete.**
> **Never mark F1-F4 as checked before getting user's okay.** Rejection or user feedback → fix → re-run → present again → wait for okay.

- [x] **F1. Plan Compliance Audit** — `oracle`
  - Verify every task in this plan has been completed against its acceptance criteria
  - Verify Definition of Done conditions all pass
  - Verify Must-NOT-Have list is honored (no mocks in production code, no scope creep, no IdC code, no UI, no caching/compression/etc.)
  - Output: APPROVED or list of violations

- [x] **F2. Code Quality Review** — `unspecified-high`
  - Run `go vet ./...`, `gofmt -l .`, `staticcheck ./...` (install if needed)
  - Verify all public APIs have doc comments
  - Verify no `TODO`/`FIXME` without issue refs
  - Verify error handling: no silent swallowing, no panics in request paths
  - Verify concurrency: token refresh has double-checked locking, account manager has proper RWMutex
  - Output: APPROVED or list of issues

- [x] **F3. Real Manual QA** — `unspecified-high`
  - Build both binaries: `go build ./cmd/server && go build ./cmd/cli`
  - Run server, exercise full flow:
    - Add account via CLI → verify SQLite row
    - Add account via REST `POST /admin/accounts` → verify row
    - Add account via JSON file edit → verify fsnotify reload + SQLite row
    - `curl /v1/messages` (Anthropic streaming, non-streaming) — happy path requires real Kiro creds; in CI use a stub Kiro server (`httptest.Server` recording fixture)
    - `curl /v1/chat/completions` (OpenAI streaming, non-streaming) — same
    - `curl /admin/quota` — verify JSON shape
    - Disable an account → verify it is skipped
    - SIGTERM → verify graceful shutdown < 30s
  - Output: APPROVED or repro steps for failures

- [x] **F4. Scope Fidelity Check** — `deep`
  - Compare delivered features against draft scope IN/OUT
  - Verify all 5 anti-ban techniques are implemented and exercised by tests
  - Verify all 3 account-add channels work (CLI, REST, file watch)
  - Verify only Social + API Key auth (no IdC code paths)
  - Output: APPROVED or list of deviations

## Commit Strategy
- One commit per task with message format: `feat(<scope>): <description>` or `fix/test/docs/refactor` for non-feat work
- Commit message references task ID: e.g., `feat(kiro): implement AWS Event Stream parser (T11)`
- Commit only after task acceptance criteria pass AND tests pass
- Push to a new branch (e.g., `feat/kiro-let-go-mvp`); never push to main
- Final commit: `chore: finalize kiro-let-go MVP (T40)` followed by PR creation

## Success Criteria
- All 40 implementation tasks (T1-T40) completed with acceptance criteria met
- All 4 final verification wave tasks (F1-F4) APPROVED by their respective review agents
- User explicitly approves the consolidated verification results
- Both binaries build, vet, and pass tests
- A real-world streaming request through the gateway returns valid Anthropic / OpenAI SSE
- Adding an account via any of the 3 channels makes it immediately available
- All 5 anti-ban techniques are observable in network traffic / logs
- Quota inspection works via REST and CLI
