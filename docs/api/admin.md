# Admin REST API

## Authentication

Tat ca endpoint duoi `/admin/*` yeu cau:

```
Authorization: Bearer <AdminAPIKey>
```

Neu thieu hoac sai key, tra 401 voi `authentication_error`.

## Endpoints

| Method | Path | Body | Response |
|--------|------|------|----------|
| POST | `/admin/accounts` | `createAccountRequest` | `accountResponse` (201) |
| GET | `/admin/accounts` | Query params | Array `accountResponse` |
| GET | `/admin/accounts/:id` | — | `getAccountResponse` |
| PATCH | `/admin/accounts/:id` | Partial JSON | `accountResponse` |
| DELETE | `/admin/accounts/:id` | — | 204 No Content |
| POST | `/admin/accounts/:id/refresh` | — | `accountResponse` |
| GET | `/admin/accounts/:id/quota` | Query `?force=` | `quotaResponse` |
| GET | `/admin/quota` | — | Array `quotaSummaryResponse` |

## Secret redaction

Cac field nhay cam bi redacted trong response:

- `refresh_token` — tra `null` (khong hien thi)
- `api_key` — tra `null` (khong hien thi)
- `proxy_password` — tra `null` (khong hien thi)
- `access_token` — van hien thi (khong bi redacted)

## Immutable fields

Sau khi tao account, field `auth_method` khong the update. Cac field khac co the PATCH duoc.

## Validation rules

### POST /admin/accounts

- `label` — bat buoc, khong duoc rong sau khi trim.
- `auth_method` — phai la `"social"` hoac `"apikey"` (hoac alias `"oauth"`, `"api_key"`).
- Neu `auth_method` la `"social"`: `refresh_token` bat buoc.
- Neu `auth_method` la `"apikey"`: `api_key` bat buoc, phai bat dau bang `ksk_`.
- `enabled` — mac dinh `true` neu khong cung cap.
- `region` — khong bat buoc, duoc trim space.

### PATCH /admin/accounts/:id

- Chi cho phep cac field: `label`, `enabled`, `proxy_url`, `proxy_username`, `proxy_password`, `region`, `auth_region`, `api_region`.
- Body khong duoc rong (phai co it nhat mot field).
- `label` — neu co, khong duoc rong sau khi trim.

### GET /admin/accounts

- Query `enabled_only` — phai la `"true"` hoac `"false"` neu co.
- Query `auth_method` — phai hop le (`social` hoac `apikey`).

### GET /admin/accounts/:id/quota

- Query `force` — phai la `"true"` hoac `"false"` neu co. Neu `true`, bypass cache va goi upstream.

## Error responses

| Status | Type | Truong hop |
|--------|------|------------|
| 400 | `validation_error` | Body khong hop le, field thieu, format sai |
| 401 | `authentication_error` | Admin API key khong hop le hoac thieu |
| 404 | `not_found_error` | Account ID khong ton tai |
| 500 | `internal_error` | Loi server noi bo (store chua configured, DB loi, ...) |

Error shape:

```json
{
  "error": {
    "type": "validation_error",
    "message": "label is required"
  }
}
```

## Curl examples

### Tao account (social auth)

```bash
curl -X POST http://localhost:8765/admin/accounts \
  -H "Authorization: Bearer <AdminAPIKey>" \
  -H "Content-Type: application/json" \
  -d '{
    "label": "my-account",
    "auth_method": "social",
    "refresh_token": "<token>",
    "region": "us-east-1",
    "enabled": true
  }'
```

### Tao account (apikey auth)

```bash
curl -X POST http://localhost:8765/admin/accounts \
  -H "Authorization: Bearer <AdminAPIKey>" \
  -H "Content-Type: application/json" \
  -d '{
    "label": "my-apikey-account",
    "auth_method": "apikey",
    "api_key": "ksk_xxxxxxxxxxxxxxxxxxxxxx",
    "enabled": true
  }'
```

### List accounts

```bash
curl "http://localhost:8765/admin/accounts?enabled_only=true" \
  -H "Authorization: Bearer <AdminAPIKey>"
```

### Get single account

```bash
curl http://localhost:8765/admin/accounts/<account-id> \
  -H "Authorization: Bearer <AdminAPIKey>"
```

### Update account

```bash
curl -X PATCH http://localhost:8765/admin/accounts/<account-id> \
  -H "Authorization: Bearer <AdminAPIKey>" \
  -H "Content-Type: application/json" \
  -d '{
    "label": "updated-label",
    "enabled": true
  }'
```

### Delete account

```bash
curl -X DELETE http://localhost:8765/admin/accounts/<account-id> \
  -H "Authorization: Bearer <AdminAPIKey>"
```

### Force refresh token

```bash
curl -X POST http://localhost:8765/admin/accounts/<account-id>/refresh \
  -H "Authorization: Bearer <AdminAPIKey>"
```

### Get quota for one account

```bash
curl "http://localhost:8765/admin/accounts/<account-id>/quota?force=true" \
  -H "Authorization: Bearer <AdminAPIKey>"
```

### Get quota summary (all accounts)

```bash
curl http://localhost:8765/admin/quota \
  -H "Authorization: Bearer <AdminAPIKey>"
```

Luu y: endpoint `/admin/quota` khong tu dong refresh quota tu upstream. No chi tra du lieu tu cache hoac DB. Day la tinh nang "probe avoidance" — tranh goi len Kiro khong can thiet.

## New endpoints

### POST /admin/accounts/:id/test

- Mô tả: Test connection tới Kiro (dùng getUsageLimits, không tốn quota)
- Response: `{status: "valid"|"banned"|"suspended"|"token_expired"|"error", message, subscription_title, user_id, duration_ms}`

### POST /admin/accounts/:id/chat-test

- Mô tả: Gửi message thật qua account cụ thể (dùng generateAssistantResponse)
- Body: `{model: "claude-haiku-4.5", message: "Hi"}`
- Response: `{success, model, message, response, duration_ms, error}`

### GET /admin/accounts/:id/models

- Mô tả: Lấy danh sách models từ Kiro ListAvailableModels API (cache 30 phút)
- Response: `{models: [{model_id, model_name, description, rate_multiplier, ...}], default_model, cached}`

### POST /admin/accounts/:id/reset-circuit

- Mô tả: Reset circuit breaker cho account (clear failures, re-enable)
- Response: updated account + circuit state

### GET /admin/models

- Mô tả: Aggregated model list từ first enabled account
- Response: `{models: [...]}`

### GET /admin/settings

- Mô tả: Dynamic config hiện tại (load_balancer, failover, quota, model_mappings)
- Response: DynamicSettings object

### PUT /admin/settings

- Mô tả: Update dynamic config (hot-reload, không cần restart)
- Body: DynamicSettings object
- Response: updated settings

### GET /admin/proxy/config

- Mô tả: Proxy configuration + account counts + endpoint list
- Response: `{host, port, strategy, sticky_session, max_attempts, ..., endpoints: [...]}`

### GET /admin/proxy/log

- Mô tả: Request log (newest first, max 100 entries)
- Query: `?limit=50`
- Response: `[{id, timestamp, method, path, model, input_tokens, output_tokens, status, duration_ms, account_id, account_label, ...}]`

### POST /admin/proxy/test-api

- Mô tả: Test proxy flow end-to-end (qua load balancer + failover)
- Body: `{format: "anthropic"|"openai", model: "claude-haiku-4.5", message: "Hi"}`
- Response: `{success, format, model, response, duration_ms, input_tokens, output_tokens, account_label, error}`
