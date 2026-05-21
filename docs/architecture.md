# Kiến trúc kiro-let-go

## Mục tiêu

Tài liệu này mô tả kiến trúc high-level của `kiro-let-go`, cách các request đi qua hệ thống, mối quan hệ giữa các package, cơ chế concurrency và vòng đợi của ứng dụng.

## Bird's-eye view

Luồng xử lý một request chat từ client đến Kiro upstream:

```
client
  |
  v
gin engine (HTTP Server)
  |
  v
handler (Anthropic / OpenAI)
  |
  v
converter (Anthropic/OpenAI -> Normalized -> KiroPayload)
  |
  v
dispatcher (kiro.Dispatcher)
  |
  v
account.Manager (Acquire + Refresh)
  |
  v
kiro.Client (HTTP POST to AWS Event Stream)
  |
  v
AWS Event Stream response
  |
  v
StreamDecoder (binary frames -> StreamEvent)
  |
  v
SSE writer -> client
```

Với non-streaming, `Dispatcher.Once` gom tất cả event thành một `FullResponse` rồi converter chuyển về định dạng client yêu cầu.

## Layer breakdown

| Layer | Package chính | Nhiệm vụ |
|-------|--------------|----------|
| HTTP layer | `internal/server` | Khởi tạo gin engine, đăng ký middleware, route request đến đúng handler. |
| API layer | `internal/api/anthropic`, `internal/api/openai`, `internal/api/admin` | Validate request body, gọi converter, trả response theo đúng format API. |
| Conversion layer | `internal/converter` | Chuyển request từ Anthropic/OpenAI về `NormalizedRequest`, rồi sang `KiroPayload`. Ngược lại, chuyển `StreamEvent` về SSE chunk. |
| Orchestration layer | `internal/kiro` | `Dispatcher` điều phối request đến upstream: chọn account, retry với backoff, decode stream, phân loại lỗi. |
| Account layer | `internal/account` | `Manager` chọn account qua balancer + circuit breaker, refresh token, ghi nhận success/failure. `Store` CRUD trên SQLite. |
| Protocol layer | `internal/kiro` | `Client` gửi HTTP request đến Kiro endpoint. `StreamDecoder` đọc AWS Event Stream binary frames. ` antiban` build header và machine ID. |
| Storage layer | `internal/account` | SQLite với WAL mode, lưu accounts, quota cache và migration state. |

## Dependency graph

```
cmd/server/main.go
    |
    +-- internal/config
    +-- internal/logging
    +-- internal/server
    |       +-- internal/api/anthropic
    |       |       +-- internal/converter
    |       +-- internal/api/openai
    |       |       +-- internal/converter
    |       +-- internal/api/admin
    |       +-- internal/server/middleware
    +-- internal/kiro
    |       +-- internal/account
    |       +-- internal/antiban
    |       +-- internal/errs
    +-- internal/account
    |       +-- internal/errs
    +-- internal/antiban
```

Server phụ thuộc vào `account.Manager`, `kiro.Dispatcher` và các handler API. `kiro.Dispatcher` phụ thuộc vào `kiro.Client` và `account.Manager`. `account.Manager` phụ thuộc vào `account.Store`, balancer, circuit breaker và auth refreshers. Không có package nào import vòng tròn.

## Concurrency model

**Token refresh DCL (Double-Checked Locking)**

Mỗi account có một `sync.Mutex` riêng lưu trong `sync.Map` (`tokenLocks`). Khi `Manager.acquireAccount` cần refresh, nó lock per-account, kiểm tra lại điều kiện `needsRefresh` bên trong lock để tránh nhiều goroutine cùng refresh một token. Đây là pattern DCL cơ bản.

**RWMutex trên Manager**

`Manager` dùng `sync.RWMutex` để bảo vệ `lastSuccessfulID` cho sticky session. Đọc dùng `RLock`, ghi dùng `Lock`. Các thao tác chọn account (`Acquire`) không bị block bởi sticky read, nhưng update sticky chỉ xảy ra trong `ReleaseSuccess` callback.

**errgroup cho shutdown**

Trong `cmd/server/main.go`, `runApplication` tạo một `errgroup` gồm 3 goroutine:

1. Chạy HTTP server (`app.server.Run`).
2. Chạy credentials watcher (`app.watcher.Run`) nếu được cấu hình.
3. Lắng nghe SIGINT/SIGTERM, gọi `cancel()` khi nhận tín hiệu.

`group.Wait()` đợi tất cả goroutine kết thúc. Nếu shutdown xảy ra do signal và có `context.DeadlineExceeded`, ứng dụng trả về exit code 2 (unclean shutdown).

**Dispatcher retry**

`Dispatcher.Stream` chạy trong một goroutine của caller (handler). Retry loop là synchronous: mỗi attempt gọi `Manager.Acquire`, gửi request, nếu lỗi recoverable thì backoff rồi thử lại. Không spawn goroutine mới cho từng attempt. Stream output được forward qua channel `out` với buffer `streamChannelCapacity`.

## Lifecycle

### Startup

```
parse CLI flags
  |
  v
config.LoadWithFlags (defaults -> JSON -> env -> flags)
  |
  v
config.Validate (bắt buộc AdminAPIKey và ProxyAPIKey)
  |
  v
init logger
  |
  v
ensure SQLite parent directory tồn tại
  |
  v
account.OpenDB -> account.Apply migrations (idempotent)
  |
  v
account.NewStore
  |
  v
seed circuit breaker từ failure_count trong DB
  |
  v
kiro.NewSocialAuth + kiro.NewAPIKeyAuth + kiro.NewClient
  |
  v
account.NewFetcher + account.NewBalancer -> account.NewManager
  |
  v
kiro.NewDispatcher
  |
  v
server.New (wire-up handlers)
  |
  v
account.NewWatcher (nếu credentials_json_path được set)
  |
  v
errgroup.Run: server + watcher + signal listener
```

### Shutdown

Khi nhận SIGTERM hoặc SIGINT:

1. Signal goroutine gọi `cancel()` trên run context.
2. `server.Run` thấy `<-ctx.Done()`, tạo shutdown context với timeout 30 giây, gọi `http.Server.Shutdown`.
3. `http.Server.Shutdown` dừng listener, đợi các request đang xử lý hoàn tất hoặc hết timeout.
4. Các streaming request đang chạy nhận `ctx.Done()`, forward error qua stream rồi đóng connection.
5. Watcher goroutine cũng nhận `ctx.Done()` và thoát.
6. `group.Wait()` trả về. Nếu timeout xảy ra trước khi server shutdown xong, trả về lỗi `unclean shutdown`.
7. `application.close()` đóng `account.Store` và `sql.DB` theo thứ tự.

Build target được định nghĩa trong [Makefile](../../Makefile):

```makefile
build:
	go build -ldflags "..." -o bin/kiro-let-go ./cmd/server
	go build -ldflags "..." -o bin/kiro-let-go-cli ./cmd/cli
```

## Admin UI

Giao diện quản trị được xây dựng bằng React 19 + Vite + TypeScript + Zustand + shadcn/ui.

- Build: `cd web && npm ci && npm run build`
- Output được embed vào binary qua `embed.FS`
- Served tại `/admin/ui/`

Admin UI cung cấp các tab: Accounts (CRUD + test connection), Models (xem và map models), Settings (edit dynamic config trực tiếp), Proxy Log (request history), và Test API (end-to-end proxy test).

## Dynamic Config Layer

Cấu hình động được lưu trong bảng `settings` của SQLite. Static config (JSON file) dùng cho bootstrap, dynamic config (DB) dùng cho runtime.

- Hot-reload qua `DynamicConfig.Get()` mỗi request
- `RWMutex` bảo vệ, chi phí đọc rất thấp
- Các components đọc config mới nhất mà không cần restart

## Token Auto-Refresh

Background goroutine chạy với interval 5 phút. Tự động refresh social tokens 10 phút trước khi hết hạn. Nếu token hết hạn và refresh thất bại, account sẽ bị auto-disable.

## Reliability Pipeline

Trước khi gửi request tới Kiro, dispatcher chạy pipeline sau:

1. **Schema Normalization** — Chuẩn hóa request về định dạng internal
2. **Tool Name Shortening** — Rút gọn tên tool nếu vượt quá giới hạn
3. **Payload Size Guard** — Tự động trim nếu payload vượt quá 600KB

## Circuit Breaker

Ngưỡng: 3 failures. Khi một account đạt ngưỡng, circuit breaker mở và account bị loại khỏi rotation. Auto-recovery khi tất cả accounts đều bị block (thử lại với xác suất `probabilistic_retry_chance`).

## Model Resolution

Luồng resolve model từ input tới upstream:

```
normalizeModelInput()
  |
  v
MapModel()
  |
  v
ModelMapper.Resolve() (DB rules)
  |
  v
fallback chains
```

`normalizeModelInput()` chuẩn hóa tên model từ client. `MapModel()` áp dụng mapping rules từ dynamic config. `ModelMapper.Resolve()` tra cứu DB để tìm target model. Nếu không match rule nào, dùng fallback chains.
