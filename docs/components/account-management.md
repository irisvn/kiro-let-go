# Account Management

## Domain model

`Account` la struct trung tam dai dien cho mot Kiro service account:

```go
type Account struct {
    ID             string
    Label          string
    AuthMethod     string        // "social" hoac "apikey"
    AccessToken    *string
    RefreshToken   *string
    APIKey         *string
    ExpiresAt      *time.Time
    ProfileARN     *string
    Region         string        // default "us-east-1"
    AuthRegion     *string
    APIRegion      *string
    MachineID      string        // SHA256 hex, stable per account
    ProxyURL       *string
    ProxyUsername  *string
    ProxyPassword  *string
    Enabled        bool
    DisabledReason *string
    FailureCount   int           // so lan fail lien tiep
    LastFailureAt  *time.Time
    SuccessCount   int           // so lan success ke tu startup
    LastUsedAt     *time.Time
    CreatedAt      time.Time
    UpdatedAt      time.Time
}
```

Cac truong quan trong:

- `MachineID`: duoc tinh tu `SHA256(label + "KiroIDE-MachineID-v1")` hoac `SHA256(id + salt)` neu label trong. Gia tri nay on dinh, khong regenerate.
- `FailureCount` / `SuccessCount`: dung cho load balancer va circuit breaker.
- `ProfileARN`: optional, neu co se duoc inject vao Kiro payload.
- `AuthRegion` / `APIRegion`: cho phep override region rieng cho auth refresh va API calls.

---

## SQLite schema

Database su dung driver pure-Go `modernc.org/sqlite` (khong can CGO). DSN tu dong append cac pragmas:

```
_pragma=journal_mode(WAL)
_pragma=busy_timeout(5000)
_pragma=foreign_keys(1)
```

Cac bang:

**accounts**

| Cot | Kieu | Ghi chu |
|-----|------|---------|
| id | TEXT PRIMARY KEY | UUID v4 |
| label | TEXT NOT NULL | |
| auth_method | TEXT NOT NULL | "social" / "apikey" |
| access_token | TEXT | nullable |
| refresh_token | TEXT | nullable |
| api_key | TEXT | nullable |
| expires_at | TEXT | RFC3339, nullable |
| profile_arn | TEXT | nullable |
| region | TEXT NOT NULL DEFAULT 'us-east-1' | |
| auth_region | TEXT | nullable |
| api_region | TEXT | nullable |
| machine_id | TEXT NOT NULL | |
| proxy_url | TEXT | nullable |
| proxy_username | TEXT | nullable |
| proxy_password | TEXT | nullable |
| enabled | INTEGER NOT NULL DEFAULT 1 | 0/1 |
| disabled_reason | TEXT | nullable |
| failure_count | INTEGER NOT NULL DEFAULT 0 | |
| last_failure_at | TEXT | RFC3339, nullable |
| success_count | INTEGER NOT NULL DEFAULT 0 | |
| last_used_at | TEXT | RFC3339, nullable |
| created_at | TEXT NOT NULL | RFC3339 |
| updated_at | TEXT NOT NULL | RFC3339 |

Indexes: `idx_accounts_enabled`, `idx_accounts_auth_method`.

**quota_cache**

| Cot | Kieu | Ghi chu |
|-----|------|---------|
| account_id | TEXT PRIMARY KEY | FK accounts(id) ON DELETE CASCADE |
| payload_json | TEXT NOT NULL | raw JSON tu Kiro |
| fetched_at | TEXT NOT NULL | RFC3339 |

**_migrations**

| Cot | Kieu | Ghi chu |
|-----|------|---------|
| version | INTEGER PRIMARY KEY | |
| applied_at | TEXT NOT NULL | RFC3339 |

---

## Store CRUD methods

Tat ca methods deu dung prepared statements va transactions voi isolation `SERIALIZABLE`.

| Method | Mo ta |
|--------|-------|
| `Create(ctx, *Account)` | Insert account moi. Tu dong generate UUID v4 neu ID trong. Set CreatedAt/UpdatedAt. |
| `Get(ctx, id)` | Lay account theo ID. Tra ve `ErrNotFound` neu khong ton tai. |
| `List(ctx, ListFilter)` | Lay danh sach. Ho tro filter `EnabledOnly` va `AuthMethod`. |
| `Update(ctx, *Account)` | Update toan bo fields. Tra ve `ErrNotFound` neu 0 row affected. |
| `Delete(ctx, id)` | Xoa account theo ID. Tra ve `ErrNotFound` neu 0 row affected. |
| `RecordSuccess(ctx, id)` | Atomic: `success_count++`, `last_used_at=now()`, `failure_count=0`. |
| `RecordFailure(ctx, id, reason)` | Atomic: `failure_count++`, `last_failure_at=now()`. |
| `SetEnabled(ctx, id, enabled, reason)` | Update enabled va disabled_reason. |
| `UpsertQuota(ctx, *QuotaCache)` | Insert hoac replace quota cache (ON CONFLICT). |
| `GetQuota(ctx, accountID)` | Lay quota cache. Tra ve `ErrNotFound` neu chua co. |

---

## Migrations

Migration files nam trong `internal/account/migrations/*.sql`, duoc embed bang `//go:embed migrations/*.sql`. Quy trinh:

1. Doc toan bo file `.sql` trong thu muc migrations.
2. Sort theo so version (prefix truoc dau `_`).
3. Kiem tra tung version trong `_migrations`.
4. Neu chua applied, chay trong transaction: execute SQL → insert vao `_migrations` → commit.

Migrations la idempotent (su dung `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`). Duoc apply tu dong khi `OpenDB` + `Apply` duoc goi luc startup.

---

## Account Manager

`Manager` la lop trung tam dieu phoi account acquisition, token refresh, va health tracking.

### Selection flow

1. `List(ctx, EnabledOnly: true)` lay toan bo enabled accounts.
2. `filterCandidates`: loai tru excluded IDs, circuit-open accounts (tru khi probabilistic retry cho phep), va free-tier accounts khi model la Opus.
3. Neu `StickySession` bat va `lastSuccessfulID` nam trong candidates, tra ve account do ngay.
4. Nguoc lai, goi `balancer.Pick()` de chon account.

### Token refresh (Double-Checked Locking)

Moi account co mot `sync.Mutex` rieng duoc luu trong `tokenLocks` (`sync.Map`). Pattern DCL:

```go
lockAny, _ := m.tokenLocks.LoadOrStore(acc.ID, &sync.Mutex{})
lock := lockAny.(*sync.Mutex)
lock.Lock()
defer lock.Unlock()

// Re-read account tu DB de lay state moi nhat
fresh, err := m.store.Get(ctx, acc.ID)
// Neu khong force va token con han, tra ve ngay
// Nguoc lai, goi socialAuth.Refresh hoac apiKeyAuth.Refresh
```

Dieu nay ngan chan race condition khi nhieu request dong thoi can refresh cung mot account.

### Acquisition lifecycle

`Acquisition` tra ve cho caller gom `Account`, `Token`, `Region`, va hai callbacks:

- `ReleaseSuccess()`: goi `RecordSuccess` + `circuit.RecordSuccess` + `balancer.Advance()` + cap nhat `lastSuccessfulID` (neu sticky).
- `ReleaseFailure(reason)`: goi `RecordFailure` + `circuit.RecordFailure(id, reason)`.

Sticky session duoc luu in-memory (`lastSuccessfulID`), khong persist xuong DB. Muc dich la giu cung mot account cho cac request lien tiep trong cung mot conversation.

---

## Quota Fetcher

`Fetcher` chi fetch quota khi co yeu cau tuong minh. Khong co goroutine background polling.

```go
func (f *Fetcher) Get(ctx context.Context, acc *Account, force bool) (*Quota, error)
```

Logic:

1. Neu `force == false`, kiem tra cache. Neu con fresh (`time.Since(fetchedAt) < ttl`), tra ve cache.
2. Neu cache miss hoac `force == true`, goi upstream `GET /getUsageLimits`.
3. Parse response JSON, normalize thanh `Quota` struct.
4. Upsert vao `quota_cache` bang `Store.UpsertQuota`.
5. Tra ve `Quota`.

TTL mac dinh la 12 gio (`quota.cache_ttl_seconds = 43200`).

`Summary()` lay quota cho toan bo accounts nhung chi dung cache co san, khong bao gio trigger upstream fetch.

---

## JSON file watcher

`Watcher` dong bo hoa account store voi mot file JSON khai bao (declarative) tren disk.

### Cach hoat dong

1. `sync()` doc file va reconcile ngay khi `Run()` bat dau.
2. Dang ky `fsnotify.Watcher` tren **parent directory** cua file (khong phai tren file truc tiep). Dieu nay xu ly duoc cac pattern save cua editor (write temp → rename).
3. Debounce: moi event trigger mot timer 500ms. Neu co event moi trong khoang do, reset timer. Chi `sync()` khi timer fire.
4. `sync()` parse JSON array, sau do `reconcile()` trong mot SQLite transaction.

### Reconciliation

Voi moi entry trong file JSON:

- Neu `_delete: true` va co `id`: xoa account.
- Tim account hien co bang `id`. Neu khong co id, tim bang `(auth_method, refresh_token)` hoac `(auth_method, api_key)`.
- Neu tim thay: update fields (chi update nhung field co trong JSON, dung `has(field)` check).
- Neu khong tim thay: tao account moi voi UUID v4 va generate `MachineID`.

Flag dac biet:

- `_delete: true` — xoa account theo ID.
- `_remove_unlisted: true` — xoa bat ky account nao khong xuat hien trong file JSON.

---

## 3 kenh them account

| | CLI | REST API | JSON file watch |
|---|---|---|---|
| **Interface** | `kiro-let-go-cli account add` | `POST /admin/accounts` | File JSON tren disk |
| **Yeu cau auth** | Khong (truy cap truc tiep DB) | Admin API key (Bearer) | Khong |
| **MachineID** | Tu dong generate | Tu dong generate | Tu dong generate |
| **Realtime sync** | Ngay lap tuc (viet DB truc tiep) | Ngay lap tuc (HTTP response) | Debounce 500ms sau khi file thay doi |
| **Batch operations** | Khong ho tro | Khong ho tro | Co, ca array |
| **Delete / cleanup** | `account delete <id>` | `DELETE /admin/accounts/<id>` | `_delete: true`, `_remove_unlisted: true` |
| **Use case** | Setup lan dau, automation scripts | Dynamic management, integrations | GitOps, declarative config |
