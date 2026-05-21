# HTTP Server

## Mục đích

Day la entry point cua HTTP server, dung `gin` engine de xu ly request. Server nay expose cac endpoint tuong thich voi Anthropic va OpenAI API, dong thoi cung cap admin REST API de quan ly account.

## Engine setup

Server khoi tao bang `gin.New()` (khong dung `gin.Default()`) de co toan quyen kiem soat middleware stack:

```go
r := gin.New()
gin.RedirectTrailingSlash = false

r.Use(
    antiban.HealthProbeMiddleware(),
    middleware.RequestIDMiddleware(),
    middleware.LoggingMiddleware(deps.Logger),
    middleware.RecoverMiddleware(deps.Logger),
    middleware.CORSMiddleware(),
)
```

`gin.RedirectTrailingSlash = false` ngan chan gin tu dong redirect cac request co trailing slash (vi du `/admin/ui/` → `/admin/ui`), giup SPA routing hoat dong dung.

Luu y: khong dung `gin.Default()` vi no tu dong gan Logger va Recovery middleware, trong khi project can dung custom implementations.

## Middleware pipeline

Thu tu middleware duoc ap dung cho moi request:

| Thu tu | Middleware | Mo ta |
|--------|-----------|-------|
| 1 | `HealthProbeMiddleware` | Tu dong tra 200 OK cho health probe tu load balancer, khong ghi log |
| 2 | `RequestIDMiddleware` | Gan `X-Request-ID` (lay tu header hoac tao UUID moi), luu vao context |
| 3 | `LoggingMiddleware` | Ghi log moi request voi method, path, status, duration, client IP, user agent, request ID |
| 4 | `RecoverMiddleware` | Bat panic, log stack trace, tra 500 voi `internal_error` |
| 5 | `CORSMiddleware` | Set CORS header (`*`, cho phep GET/POST/PUT/PATCH/DELETE/OPTIONS), tra 204 cho OPTIONS |
| - | `ProxyAuthMiddleware` | Chi cho route `/v1/*` — kiem tra `Authorization: Bearer <key>` hoac `x-api-key: <key>` |
| - | `AdminAuthMiddleware` | Chi cho route `/admin/*` — kiem tra `Authorization: Bearer <AdminAPIKey>` |
| - | `RequestLogMiddleware` | Global — ghi log chi tiet request/response cho `/v1/*` va cac route test `/admin/*/chat-test`, `/admin/*/test` |

## Routes

| Method | Path | Auth required | Handler |
|--------|------|---------------|---------|
| GET | `/health` | Khong | Tra `{"status":"ok","version":"..."}` |
| POST | `/v1/messages` | ProxyAPIKey | Anthropic Messages API (streaming + non-streaming) |
| POST | `/v1/messages/count_tokens` | ProxyAPIKey | Dem token (local estimation) |
| POST | `/v1/chat/completions` | ProxyAPIKey | OpenAI Chat Completions API (streaming + non-streaming) |
| GET | `/v1/models` | ProxyAPIKey | Danh sach model (hardcoded) |
| POST | `/admin/accounts` | AdminAPIKey | Tao account moi |
| GET | `/admin/accounts` | AdminAPIKey | List account (co filter) |
| GET | `/admin/accounts/:id` | AdminAPIKey | Xem chi tiet account + circuit breaker status |
| PATCH | `/admin/accounts/:id` | AdminAPIKey | Update cac field mutable |
| DELETE | `/admin/accounts/:id` | AdminAPIKey | Xoa account (cascade quota_cache) |
| POST | `/admin/accounts/:id/refresh` | AdminAPIKey | Force refresh token |
| GET | `/admin/accounts/:id/quota` | AdminAPIKey | Quota cua mot account |
| GET | `/admin/quota` | AdminAPIKey | Quota summary across all accounts |
| GET | `/admin/accounts/:id/models` | AdminAPIKey | List available models cua mot account (goi Kiro API thuc) |
| POST | `/admin/accounts/:id/test` | AdminAPIKey | Test connection bang `getUsageLimits` |
| POST | `/admin/accounts/:id/chat-test` | AdminAPIKey | Gui request chat thuc de kiem tra account |
| POST | `/admin/accounts/:id/reset-circuit` | AdminAPIKey | Reset circuit breaker cua mot account |
| GET | `/admin/models` | AdminAPIKey | Danh sach models duoc ho tro |
| GET | `/admin/settings` | AdminAPIKey | Lay current settings (dynamic config) |
| POST | `/admin/settings` | AdminAPIKey | Update settings runtime |
| GET | `/admin/proxy/*` | AdminAPIKey | Proxy cac request admin den Kiro upstream |

## Health endpoint

```go
r.GET("/health", func(c *gin.Context) {
    c.JSON(http.StatusOK, gin.H{"status": "ok", "version": version.Version})
})
```

- Khong yeu cau authentication.
- `version` duoc inject qua `-ldflags` khi build. Gia tri mac dinh la `"dev"`.
- Response shape: `{"status":"ok","version":"<sha>"}`.

## Graceful shutdown

```go
shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
defer cancel()
return srv.Shutdown(shutdownCtx)
```

- Deadline shutdown la 30 giay.
- `srv.Shutdown` se dong listener moi va doi cac in-flight request hoan thanh, bao gom ca streaming requests.
- Server duoc chay tren `net.Listen("tcp", addr)` thay vi goi truc tiep `srv.ListenAndServe()`, giup lay duoc actual bound address.

## Error response shape

Tat ca cac endpoint tra loi theo cung mot format:

```json
{
  "error": {
    "type": "...",
    "message": "..."
  }
}
```

Cac `type` pho bien:
- `authentication_error` — 401, key khong hop le hoac thieu
- `invalid_request_error` — 400, request body hoac parameter khong hop le
- `internal_error` — 500, loi server noi bo
- `not_found_error` — 404, resource khong ton tai

## Admin UI (React SPA)

Admin UI duoc build thanh React SPA va duoc embed vao binary Go qua `embed.FS`. Server serve cac static files tai path `/admin/ui/`:

```go
//go:embed all:web/dist
var webDist embed.FS
```

`NoRoute` handler duoc dang ky de ho tro SPA routing: moi path khong khop voi API routes se duoc serve `index.html`, cho phep React Router xu ly client-side navigation:

```go
r.NoRoute(func(c *gin.Context) {
    // Serve index.html cho SPA routes
})
```

UI truoc day dung Alpine.js, hien tai da chuyen sang React voi build pipeline qua npm.

## Makefile build-ui target

Target `build-ui` trong Makefile dam bao UI duoc build truoc khi compile Go:

```makefile
build-ui:
	cd web && npm ci && npm run build
```

Day la prerequisite cua target `build` chinh, dam bao `web/dist` ton tai truoc khi `go build` embed static assets.

## Server configuration

```go
srv := &http.Server{
    Addr:              addr,
    Handler:           s.engine,
    ReadHeaderTimeout: 10 * time.Second,
    IdleTimeout:       60 * time.Second,
}
```

- `ReadHeaderTimeout`: 10 giay — ngan request treo khi doc header.
- `IdleTimeout`: 60 giay — dong keep-alive connection neu khong co request moi.
