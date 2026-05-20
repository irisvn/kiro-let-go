# Errors

## Mục đích

Package `errs` cung cấp typed error classification để dispatcher quyết định hành vi xử lý: retry/failover với account khác, hay return lỗi về client ngay lập tức. Thay vì so sánh string hoặc status code rải rác khắp codebase, mọi lỗi đều được gắn một `Class` rõ ràng.

---

## Class enum

| Class | Ý nghĩa | Retry behavior |
|-------|---------|----------------|
| `ClassRecoverable` | Lỗi tạm thời, có thể retry | Retry/failover với account khác |
| `ClassFatal` | Lỗi client hoặc logic, không retry | Return về client ngay |
| `ClassQuotaExhausted` | Account hết quota | Failover sang account khác |
| `ClassAuthExpired` | Token hết hạn hoặc invalid | Force refresh token 1 lần, rồi failover |
| `ClassRateLimited` | Bị rate limit (429) | Failover sang account khác |
| `ClassContentTooLong` | Content vượt ngưỡng | Fatal, return lỗi client |
| `ClassNetwork` | Lỗi network (DNS, timeout, refused) | Recoverable, retry được |
| `ClassClientCanceled` | Client đã hủy request | Abort hoàn toàn, không retry |

---

## Error struct

```go
type Error struct {
    Class      Class
    Code       string
    HTTPStatus int
    Message    string
    Cause      error
}
```

- `Class`: phân loại recoverability.
- `Code`: mã lỗi machine-readable (ví dụ `"RATE_LIMITED"`, `"QUOTA_EXHAUSTED"`).
- `HTTPStatus`: status code HTTP tương ứng (nếu có).
- `Message`: mô tả human-readable.
- `Cause`: lỗi gốc, hỗ trợ `errors.Is/As` traversal.

`Error` implement interface `error` (qua `Error() string`) và `Unwrap() error`.

---

## Helpers

| Function | Chữ ký | Mô tả |
|----------|--------|-------|
| `New` | `New(class Class, code, msg string) *Error` | Tạo lỗi mới |
| `Wrap` | `Wrap(err error, class Class, msg string) *Error` | Wrap lỗi có sẵn với class mới |
| `Is` | `Is(err error, class Class) bool` | Kiểm tra err hoặc chain có class cho trước |
| `ClassOf` | `ClassOf(err error) Class` | Trích class từ err, default `ClassFatal` |

---

## FromKiroResponse(status, body) mapping

Hàm `FromKiroResponse` ánh xạ HTTP response từ Kiro thành `*Error`. Trả về `nil` cho status 200.

| Status | Body condition | Class | Message |
|--------|---------------|-------|---------|
| 200 | — | `nil` (không phải lỗi) | — |
| 402 | Chứa `MONTHLY_REQUEST_COUNT` | `ClassQuotaExhausted` | `"quota exhausted"` |
| 401 / 403 | — | `ClassAuthExpired` | `"authentication expired"` |
| 429 | — | `ClassRateLimited` | `"rate limited"` |
| 400 | Chứa `CONTENT_LENGTH_EXCEEDS_THRESHOLD` | `ClassContentTooLong` | `"content too long"` |
| 400 / 422 | (other) | `ClassFatal` | `"fatal client error"` |
| 5xx | — | `ClassRecoverable` | `"server error"` |
| other | — | `ClassFatal` | `"unknown error"` |

Lưu ý: 5xx được classify là `ClassRecoverable` vì đây là lỗi transient từ server, có thể retry.

---

## FromNetwork(err) mapping

Hàm `FromNetwork` ánh xạ network-level error. Trả về `nil` cho lỗi không nhận diện được.

| Lỗi | Class | Message |
|-----|-------|---------|
| `context.Canceled` | `ClassClientCanceled` | `"client canceled request"` |
| DNS resolution failure (`no such host`, `DNS`, `lookup ... failed`) | `ClassNetwork` | `"dns resolution failed"` |
| Connection refused (`ECONNREFUSED` hoặc string match) | `ClassNetwork` | `"connection refused"` |
| TLS handshake failure (`tls:`, `certificate`, `handshake`) | `ClassNetwork` | `"tls error"` |
| Timeout (`net.Error.Timeout()`, `ETIMEDOUT`, string `timeout`/`timed out`) | `ClassNetwork` | `"network timeout"` / `"connection timed out"` |
| Host/network unreachable (`EHOSTUNREACH`, `ENETUNREACH`) | `ClassNetwork` | `"network unreachable"` |

Đặc biệt, `context.Canceled` được xử lý riêng và classify là `ClassClientCanceled`, nghĩa là abort hoàn toàn, không retry.

---

## Usage trong dispatcher

Dispatcher sử dụng `Class` để quyết định hành động:

| Class | Hành động |
|-------|-----------|
| `ClassRecoverable`, `ClassQuotaExhausted`, `ClassRateLimited` | `ReleaseFailure` + thêm account vào `ExcludeIDs` + tiếp tục vòng loop failover |
| `ClassAuthExpired` | Force refresh token 1 lần trên cùng account, rồi retry. Nếu vẫn lỗi thì failover. |
| `ClassFatal`, `ClassContentTooLong` | `ReleaseFailure` + return lỗi về client ngay lập tức |
| `ClassClientCanceled` | Abort, không failover, không retry |

---

## Test coverage

File `internal/errs/errs_test.go` cung cấp table-driven test với 12+ combinations cho `FromKiroResponse`:

- 200 (not an error)
- 402 + `MONTHLY_REQUEST_COUNT` → `ClassQuotaExhausted`
- 401 / 403 → `ClassAuthExpired`
- 429 → `ClassRateLimited`
- 400 + `CONTENT_LENGTH_EXCEEDS_THRESHOLD` → `ClassContentTooLong`
- 400 other / 422 → `ClassFatal`
- 500 / 503 → `ClassRecoverable`
- 418 (fallback) → `ClassFatal`

Và cho `FromNetwork`:

- `nil` → `nil`
- `context.Canceled` → `ClassClientCanceled`
- DNS timeout → `ClassNetwork`
- `ECONNREFUSED` / `ETIMEDOUT` / `EHOSTUNREACH` → `ClassNetwork`
- String-match DNS, TLS, refused, timeout → `ClassNetwork`
- Unrecognized → `nil`

Các test cũng cover `New`, `Wrap`, `Is`, `ClassOf` với wrapped chains và plain errors.
