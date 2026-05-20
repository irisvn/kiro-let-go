## F4 Scope Fidelity Check re-run - 2026-05-20

Result: APPROVED.

Verification run: `go test ./...` PASS.

### Anti-ban techniques
- Per-account machine ID: implemented by deterministic SHA-256 generator in `internal/kiro/machine_id.go:11-28`; CLI assigns IDs in `internal/cli/account.go:91-100`; REST assigns IDs in `internal/api/admin/handler.go:207-223`; watcher assigns IDs in `internal/account/watcher.go:207-212`. Tests: `internal/kiro/machine_id_test.go:5-44`, CLI add `internal/cli/cli_test.go:63-77`, REST create `internal/api/admin/handler_test.go:101-115`, watcher create `internal/account/watcher_test.go:37-63`.
- Request header randomization/fingerprinting: implemented with deterministic per-account version/OS plus fresh invocation ID in `internal/antiban/headers.go:12-23` and `internal/antiban/headers.go:40-58`. Tests: `internal/antiban/headers_test.go:13-33`, `internal/antiban/headers_test.go:45-65`.
- Per-account proxy: implemented by per-account cached proxy clients/transports in `internal/kiro/client.go:61-85`, HTTP/SOCKS proxy setup in `internal/kiro/client.go:88-118`, and credential injection in `internal/kiro/client.go:121-139`; social refresh uses account proxy in `internal/kiro/auth_social.go:71-78` and `internal/kiro/auth_social.go:112-130`. Tests: `internal/kiro/client_test.go:43-83`, `internal/kiro/auth_social_test.go:227-252`.
- Health probe avoidance: implemented in `internal/antiban/probe.go:10-25` for probe identifiers/paths, `internal/antiban/probe.go:27-46` for detection, `internal/antiban/probe.go:48-60` for short-circuit middleware; wired before auth/routes in `internal/server/server.go:44-45`. Tests: `internal/antiban/probe_test.go:15-56`.
- Failure-based cooldown/circuit breaker: implemented in `internal/account/circuit.go:60-85` for open/cooldown/probabilistic retry, `internal/account/circuit.go:99-114` for failure recording, `internal/account/circuit.go:146-170` for exponential backoff and probabilistic retry; manager excludes open circuits and records outcomes in `internal/account/manager.go:158-160` and `internal/account/manager.go:204-223`. Tests: `internal/account/circuit_test.go:26-200`, manager release test `internal/account/manager_test.go:174-198`.

### Account-add channels
- CLI (`kiro-let-go-cli account add`): command and flags in `internal/cli/account.go:42-88`; create path/validation/storage/optional social refresh in `internal/cli/account.go:91-157`. Tests: `internal/cli/cli_test.go:63-101`.
- REST Admin API (`POST /admin/accounts`): route in `internal/api/admin/handler.go:145-148`; handler validation/create/refresh in `internal/api/admin/handler.go:157-255`. Tests: `internal/api/admin/handler_test.go:87-138`, validation/refresh-failure tests `internal/api/admin/handler_test.go:140-175`.
- JSON file watch (`fsnotify`): watcher creation/directory watch in `internal/account/watcher.go:54-63`; fsnotify debounce loop in `internal/account/watcher.go:81-112`; reconcile create/update/delete in `internal/account/watcher.go:173-236`; server wiring in `cmd/server/main.go:171-176` and `cmd/server/main.go:197-200`. Tests: `internal/account/watcher_test.go:16-63`, `internal/account/watcher_test.go:65-96`, `internal/account/watcher_test.go:98-123`.

### Auth scope
- Social refreshToken auth: `internal/kiro/auth_social.go:36-110`, tests `internal/kiro/auth_social_test.go:21-252`.
- API Key auth: `internal/kiro/auth_apikey.go:15-27`, tests `internal/kiro/auth_apikey_test.go:14-39`.
- Runtime wires only Social + API Key auth: `cmd/server/main.go:137-158`; manager dispatches API-key vs social refresh only in `internal/account/manager.go:241-273`.
- Search for `StartDeviceAuthorization`, `CreateToken`, `ssooidc`, `identitycenter`, `SSO`, `OIDC`, `IdC` found no Go implementation paths. `oauth` appears only as a legacy alias/comment/test label that maps to social (`internal/api/admin/handler.go:624-629`) or social/default refresh handling (`internal/account/manager.go:241-273`), not IdC/AWS SSO OIDC.

### Load balancer, sticky session, quota
- Load balancer strategies match plan: `round_robin`, `balanced`, `most_quota` in `internal/account/balancer.go:164-174`; strategy behavior in `internal/account/balancer.go:20-42`, `internal/account/balancer.go:44-73`, `internal/account/balancer.go:75-162`. Tests: `internal/account/balancer_test.go:14-250`.
- Sticky session is implemented/enabled path: candidate reuse in `internal/account/manager.go:101-103` and `internal/account/manager.go:169-185`; last-success tracking in `internal/account/manager.go:212-215`; test `internal/account/manager_test.go:103-130`.
- Quota inspection via REST: routes `internal/api/admin/handler.go:153-154`; handlers `internal/api/admin/handler.go:463-509`; tests `internal/api/admin/handler_test.go:242-298`.
- Quota inspection via CLI: command and dispatch `internal/cli/quota.go:23-39`; summary/single implementations `internal/cli/quota.go:41-119`; output `internal/cli/quota.go:121-176`.

### Notes
- `internal/kiro/auth.go` does not exist; auth code is split across `auth_social.go` and `auth_apikey.go`.
- Grep terms run: `machine`, `header`, `proxy`, `probe`, `cooldown`, `circuit`, `breaker`, `fsnotify`, `IdC`, `SSO`, `OIDC` plus AWS SSO/OIDC API names.
