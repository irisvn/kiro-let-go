# Anti-Ban Techniques

## Tổng quan

Proxy ap dung 5 ky thuat anti-ban chinh de mo phong traffic tu Kiro IDE thuc, giup cac account ton tai lau hon va giam nguy co bi detect la automation. Cac ky thuat nay hoat dong song song, bo sung lan nhau.

---

## Ky thuat 1: Per-account machine ID

Moi account co mot machine ID on dinh, duy nhat. ID nay duoc tinh bang SHA256 hex cua `seed + "KiroIDE-MachineID-v1"` (lowercased):

```go
func Generate(seed string) string {
    sum := sha256.Sum256([]byte(seed + machineIDSalt))
    return hex.EncodeToString(sum[:])
}
```

- `seed` la `label` cua account. Neu label trong, dung `id` lam seed.
- Machine ID duoc luu trong DB (`machine_id` column) va khong bao gio regenerate.
- Hai account khac nhau se co machine ID khac nhau, mo phong hai may tinh khac nhau goi Kiro API.
- Machine ID duoc validate: phai la chuoi hex 64 ky tu thuong.

---

## Ky thuat 2: Default profileArn injection

Voi social accounts, neu khong co `profileArn` trong database, proxy tu dong inject gia tri mac dinh:

```
arn:aws:codewhisperer:us-east-1:699475941385:profile/EHGA3GRVQMUK
```

Day la profile ARN mac dinh cua Kiro IDE, giup social accounts hoat dong ngay ma khong can cau hinh thu cong.

## Ky thuat 3: Header randomization (stable per account)

Proxy build request headers sao cho cung mot account luon gui cung fingerprint, nhung khac account thi fingerprint khac nhau. Dieu nay quan trong vi "random UA per request" lai chinh la dau hieu anomaly.

### Deterministic selection

```go
func OnceFor(accountID string, listLen int) int {
    h := fnv.New64a()
    _, _ = h.Write([]byte(accountID))
    return int(h.Sum64() % uint64(listLen))
}
```

- `User-Agent` version duoc chon tu list `{"1.0.31", "1.0.32", "1.0.33", "1.0.34"}` bang hash cua `accountID`.
- OS portion (`darwin`, `linux`, `win32`) duoc chon bang hash cua `accountID + ":os"`.
- Cung account = cung version + cung OS tren moi request.

### Cac header bat buoc

Moi request Kiro phai co du 9 header sau:

| Header | Gia tri |
|--------|---------|
| `Authorization` | `Bearer <token>` |
| `Content-Type` | `application/json` |
| `Connection` | `close` |
| `host` | `q.{region}.amazonaws.com` |
| `x-amzn-codewhisperer-optout` | `true` |
| `x-amzn-kiro-agent-mode` | `vibe` |
| `amz-sdk-invocation-id` | UUID v4 moi **per request** |
| `amz-sdk-request` | `attempt=1; max=3` |
| `tokentype` | `API_KEY` (chi khi auth method la apikey) |

Luu y: `amz-sdk-invocation-id` la header duy nhat thay doi moi request. Day la dung vi AWS SDK tuong minh tao UUID moi cho moi invocation.

User-Agent mau:

```
aws-sdk-js/1.0.34 ua/2.1 os/darwin lang/js md/nodejs#v20.10.0
api/codewhispererstreaming#1.0.34 m/E KiroIDE-1.0.34-<machine_id>
```

---

## Ky thuat 4: CRC32-IEEE cho AWS Event Stream

AWS Event Stream parser dung CRC32 voi IEEE polynomial (CRC32-IEEE), khong phai CRC32C (Castagnoli). Day la su khac biet quan trong vi checksum se khong khop neu dung sai table.

```go
var crc32Table = crc32.MakeTable(crc32.IEEE)
```

## Ky thuat 5: Minimal headers cho quota/models

Cac endpoint `getUsageLimits` va `ListAvailableModels` khong can full KiroIDE User-Agent. Chi can:

- `Authorization: Bearer <token>`
- `Content-Type: application/json`

Dieu nay tranh yeu cau `profileArn` cho cac request chi can lay thong tin co ban, giup cac tool kiem tra quota hoat dong ngay ca khi account chua co profile ARN day du.

## Ky thuat 6: Per-account proxy

Moi account co the co proxy rieng (HTTP, HTTPS, hoac SOCKS5). Proxy nay duoc cau hinh qua cac fields:

- `proxy_url`: vi du `http://proxy.example.com:8080`, `socks5://127.0.0.1:1080`
- `proxy_username`, `proxy_password`: optional basic auth

### Cach hoat dong

`kiro.Client` cache `*http.Client` rieng cho moi account trong `sync.Map`:

```go
func (c *Client) clientForAccount(acc *account.Account) *http.Client {
    key := acc.ID
    if cached, ok := c.clients.Load(key); ok {
        return cached.(*http.Client)
    }
    // Build transport voi proxy
    client := &http.Client{Timeout: 0, Transport: transport}
    actual, _ := c.clients.LoadOrStore(key, client)
    return actual.(*http.Client)
}
```

Dieu nay dam bao:

- Cac account khong bao gio chia se connection pool.
- SOCKS5 proxy ho tro ca username/password auth.
- Neu proxy URL khong hop le, client fallback ve direct transport va ghi warning.

---

## Ky thuat 7: Health-probe avoidance

Proxy chu dong tranh cac pattern giong health check / monitoring de khong tao ra traffic deu dan ma Kiro co the flag.

### On-demand quota fetch only

Quota chi duoc fetch khi:

- Admin request `GET /admin/quota` hoac `GET /admin/accounts/<id>/quota`.
- CLI command `kiro-let-go-cli quota`.
- 402 response tu Kiro (quota exhausted trigger).

**Khong co** goroutine background polling. **Khong co** scheduled health checks.

### Probabilistic retry

Circuit breaker van cho phep `10%` requests thu lai cac account dang cooldown. Day la "recovery probe" o tan thap, du de phat hien khi account da khoi phuc ma khong tao thanh "retry storm".

### Incoming probe filtering

`antiban.IsHealthProbe` detect cac request tu health probes (ELB, kube-probe, Prometheus, UptimeRobot, v.v.) bang User-Agent va path (`/healthz`, `/readyz`, `/livez`). Middleware tra ve `{"status":"ok"}` ngay, khong bao gio cham toi auth, DB, hay upstream Kiro.

---

## Ky thuat 8: Failure-based cooldown (circuit breaker)

Khi mot account lien tuc that bai, no se bi tam thoi loai khoi rotation bang exponential backoff. Chi tiet day du xem tai `load-balancing.md`. Tom tat:

- Base cooldown: 60 giay.
- Nhan doi moi lan fail: `60s → 120s → 240s → ...`
- Max cooldown: 24 gio (`60s * 1440`).
- Probabilistic retry: `10%` chance de thu lai bat ky luc nao.

Xac suat nay duoc calibrate de trong giong organic recovery, khong phai retry storm.

---

## Bonus: Sticky sessions

Khi `sticky_session` bat, proxy ghi nho `lastSuccessfulID` (in-memory) sau moi request thanh cong. Request tiep theo cung conversation se uu tien account do neu van con available.

Dieu nay giup:

- Tranh chuyen doi identity giua cac turn trong cung mot conversation.
- Duy tri consistency voi Kiro upstream (conversation state, quota tracking).
- Giam so luong account can active trong cung mot thoi diem.

Sticky session khong ghi xuong DB, chi luu trong memory cua process hien tai. Neu restart, sticky state mat di va selection se bat dau lai tu balancer.
