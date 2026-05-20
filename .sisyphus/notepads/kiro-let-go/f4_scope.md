## F4 Scope Fidelity Check - 2026-05-20

Result: NOT APPROVED — deviations found.

Verification run: `go test ./...` PASS.

### Anti-ban techniques
- Per-account machine ID: implemented. CLI creates `MachineID` via `kiro.Generate(acc.ID)` in `internal/cli/account.go:99`; REST creates via label/id seed in `internal/api/admin/handler.go:207-223`; watcher creates via `generateMachineID` in `internal/account/watcher.go:207-212` and `internal/account/watcher.go:378-384`. Tests: `internal/kiro/machine_id_test.go:5-18`, CLI `internal/cli/cli_test.go:63-77`, REST `internal/api/admin/handler_test.go:101-115`, watcher `internal/account/watcher_test.go:37-63`.
- Request header randomization/fingerprinting: implemented as deterministic per-account version/OS plus fresh invocation ID in `internal/antiban/headers.go:12-22` and `internal/antiban/headers.go:40-58`; used by dispatcher `internal/kiro/dispatcher.go:242-250` and quota fetcher `internal/account/quota.go:134-136`. Tests: `internal/antiban/headers_test.go:13-33`, `internal/antiban/headers_test.go:45-65`, quota header assertion `internal/account/quota_test.go:41-48`.
- Per-account proxy: implemented for Kiro client per-account cached transports in `internal/kiro/client.go:61-86`, HTTP/SOCKS proxy creation in `internal/kiro/client.go:88-118`, credential injection in `internal/kiro/client.go:121-139`; social refresh also builds proxied client in `internal/kiro/auth_social.go:71-78` and `internal/kiro/auth_social.go:112-130`. Tests: `internal/kiro/client_test.go:43-53`, `internal/kiro/client_test.go:55-83`, `internal/kiro/auth_social_test.go:227-252`. Deviation: `internal/antiban/proxy.go:5-6` is only a placeholder direct dialer and not used for the feature.
- Health probe avoidance: DEVIATION. `internal/antiban/probe.go:3-4` is only a placeholder; grep found server `/health` endpoints (`internal/server/server.go:45`) but no anti-ban Kiro health-probe avoidance implementation or test. Closest quota behavior avoids background probing (`internal/account/quota_test.go:140-148`) and summary avoids upstream fetch (`internal/account/quota_test.go:107-138`), but this does not exercise an explicit health-probe avoidance feature.
- Failure-based cooldown/circuit breaker: implemented. Circuit state/backoff/probabilistic retry in `internal/account/circuit.go:60-85`, `internal/account/circuit.go:99-114`, `internal/account/circuit.go:146-170`; manager excludes open circuits and records success/failure in `internal/account/manager.go:158-160`, `internal/account/manager.go:204-219`. Tests: `internal/account/circuit_test.go:26-86`, `internal/account/circuit_test.go:88-157`, `internal/account/circuit_test.go:159-200`, manager release recording `internal/account/manager_test.go:174-198`.

### Account-add channels
- CLI (`kiro-let-go-cli account add`): implemented as Cobra `account`/`add` command and flags in `internal/cli/account.go:42-88`; create path in `internal/cli/account.go:91-157`. Tests cover `runAccountAdd` for apikey/social/invalid type in `internal/cli/cli_test.go:63-101`. Deviation: tests do not execute the full Cobra command path for `kiro-let-go-cli account add`.
- REST Admin API (`POST /admin/accounts`): route in `internal/api/admin/handler.go:145-148`; handler/validation/create/refresh in `internal/api/admin/handler.go:157-255`. Tests: create/list/get/circuit in `internal/api/admin/handler_test.go:87-138`, validation/refresh-failure disable in `internal/api/admin/handler_test.go:140-175`.
- JSON file watch (`fsnotify`): `fsnotify.NewWatcher` and directory watch in `internal/account/watcher.go:54-63`; event debounce/sync in `internal/account/watcher.go:81-112`; reconcile create/update/delete in `internal/account/watcher.go:173-236`. Server wires watcher when configured in `cmd/server/main.go:163-166` and runs it in `cmd/server/main.go:189-197`. Tests: initial sync in `internal/account/watcher_test.go:16-63`, lookup/delete/invalid shape in `internal/account/watcher_test.go:65-96`, fsnotify run/debounce in `internal/account/watcher_test.go:98-123`.

### Auth scope
- Social refreshToken implemented in `internal/kiro/auth_social.go:36-110`, tests in `internal/kiro/auth_social_test.go:30-57` and error/region/proxy tests through `internal/kiro/auth_social_test.go:252`.
- API Key auth implemented in `internal/kiro/auth_apikey.go:15-27`, tests in `internal/kiro/auth_apikey_test.go:14-39`.
- Runtime wires only `NewSocialAuth` and `NewAPIKeyAuth` in `cmd/server/main.go:137-158`.
- Grep for `IdC|IDC|SSO|OIDC|StartDeviceAuthorization|CreateToken|ssooidc|identitycenter` found only plan text, no Go code paths. Deviation/alias note: code accepts legacy `oauth` as alias for social in REST `internal/api/admin/handler.go:624-629` and manager `internal/account/manager.go:320-328`; this maps to social refresh-token backend, not IdC/OIDC.

### Load balancing, sticky session, quota
- Load balancer strategies implemented: `round_robin`, `balanced`, `most_quota` in `internal/account/balancer.go:164-174`; tests in `internal/account/balancer_test.go:14-30`, round-robin tests `internal/account/balancer_test.go:32-100`, balanced tests `internal/account/balancer_test.go:102-150`, most_quota tests `internal/account/balancer_test.go:152-250`. Defaults: `round_robin`, sticky true in `internal/config/config.go:77-78`.
- Sticky session implemented in manager candidate reuse and last-success tracking in `internal/account/manager.go:101-103`, `internal/account/manager.go:169-185`, `internal/account/manager.go:210-214`; tested in `internal/account/manager_test.go:103-130`.
- Quota inspection via REST: routes in `internal/api/admin/handler.go:153-154`; handlers in `internal/api/admin/handler.go:463-517`; tests in `internal/api/admin/handler_test.go:242-298`.
- Quota inspection via CLI: command in `internal/cli/quota.go:23-39`; summary/single implementations in `internal/cli/quota.go:41-119`; output in `internal/cli/quota.go:121-176`. Deviation: no CLI quota tests found (`internal/cli/*_test.go` has no `runQuota`/quota coverage).

### Deviations summary
1. Health probe avoidance is not implemented or tested; only `internal/antiban/probe.go:3-4` placeholder exists.
2. CLI account add tests cover `runAccountAdd`, not the full Cobra command invocation path for `kiro-let-go-cli account add`.
3. CLI quota inspection is implemented but untested.
4. `internal/antiban/proxy.go:5-6` remains a placeholder, although per-account proxy behavior exists in `internal/kiro/client.go` and social auth.
