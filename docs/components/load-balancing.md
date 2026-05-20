# Load Balancing

## 3 strategies

Proxy ho tro 3 chien luoc chon account qua interface `Balancer`.

| Strategy | Thuat toan | Dieu kien advance |
|----------|-----------|-------------------|
| `round_robin` | Duyet candidates theo thu tu, chi so `idx` tang dan. Index chi tang khi co **successful use** (goi `Advance()`). | `ReleaseSuccess` trigger `Advance()`. |
| `balanced` | Chon account co `success_count` thap nhat. Neu bang nhau, chon `last_used_at` cu hon. | Khong advance. Re-compute moi lan `Pick()`. |
| `most_quota` | Chon account co `LimitRemaining` cao nhat tu quota cache. Neu cache miss hoac stale, coi nhu 0 va trigger background refresh. Neu bang nhau, tiep tuc so sanh `success_count` roi `last_used_at`. | Khong advance. Re-compute moi lan `Pick()`. |

Luu y: `most_quota` khong block `Pick()` de fetch quota. No dung cache hien co; neu khong co cache thi tra ve 0 va spawn mot goroutine background de fetch va upsert cache cho lan sau.

---

## Configuration

```json
{
  "load_balancer": {
    "strategy": "round_robin",
    "sticky_session": true
  }
}
```

- `strategy`: mac dinh `round_robin`. Co the la `balanced` hoac `most_quota`.
- `sticky_session`: neu `true`, consecutive request cung conversation se co gang dung lai account thanh cong gan nhat nhat (in-memory `lastSuccessfulID`).

---

## Circuit Breaker

`CircuitBreaker` duoc luu hoan toan trong memory, khong persist trang thai real-time xuong DB (chi seed tu `failure_count` luc startup).

### State per account

```go
type state struct {
    failures    int
    lastFailure time.Time
    lastReason  string
}
```

### Cooldown formula

```
cooldown = BaseCooldown * 2^(failures - 1)
cooldown = min(cooldown, BaseCooldown * MaxBackoffMultiplier)
```

Voi gia tri mac dinh:

- `BaseCooldown = 60s`
- `MaxBackoffMultiplier = 1440`

Day la bang cooldown theo so lan fail lien tiep:

| failures | Cooldown |
|----------|----------|
| 1 | 60s |
| 2 | 120s |
| 3 | 240s |
| 4 | 480s |
| 5 | 960s |
| 6 | 1920s |
| 7 | 3840s |
| 8 | 7680s |
| 9 | 15360s |
| 10 | 30720s |
| ... | ... |
| cap | 86400s (24h) |

### Probabilistic retry

Ngay ca khi dang trong cooldown, co `10%` (`ProbabilisticRetryChance = 0.10`) co hoi account van duoc thu lai. Day la "recovery probe" de phat hien khi account da khoi phuc ma khong can doi het cooldown.

### Startup seeding

Khi khoi dong, `Manager` load `failure_count` tu DB vao circuit breaker va set `lastFailure = now`. Dieu nay co nghia la neu mot account da fail 5 lan truoc khi restart, no se bat dau voi cooldown tuong ung ngay tu dau.

### Snapshot

`Snapshot()` tra ve map cac `CircuitInfo` cho tat ca account da co state, bao gom `CooldownEnds` tinh toan.

---

## Failover Dispatcher

`Dispatcher` dieu phoi viec gui request qua nhieu account voi retry va failover tu dong.

### Outer loop

```
for attempt := 0; attempt < MaxAttempts; attempt++ {
    // MaxAttempts mac dinh = 9
}
```

### Per attempt flow

1. **Acquire**: goi `manager.Acquire(hint)` de chon account. Neu khong co candidate, tra ve loi ngay.
2. **buildKiroRequest**: build HTTP request voi payload, token, region, va anti-ban headers.
3. **client.Stream**: gui request qua `kiro.Client`. Neu network error, classify va retry.
4. **Status check**: neu status khong phai 200:
   - `402` (quota exhausted) → `ReleaseFailure`, exclude account, continue.
   - `401/403` (auth expired) → force-refresh token mot lan, sau do exclude neu van fail.
   - `429` (rate limited) → `ReleaseFailure`, exclude, continue.
   - `5xx` → recoverable, retry.
   - `400/422` → fatal, return to client ngay.
5. **Streaming success**: neu body nhan duoc thanh cong, spawn goroutine de forward stream events. Sau byte dau tien, **khong con failover** nua. Goroutine se goi `ReleaseSuccess` khi stream ket thuc hoac `ReleaseFailure` neu co loi mid-stream.

### Backoff giua cac attempts

```go
d := BaseRetryMs * 2^attempt
d = min(d, 2000ms)
d += jitter(d/4)
```

Voi `BaseRetryMs = 100`, day la cac khoang backoff (tinh ca jitter):

| attempt | Backoff ~ |
|---------|-----------|
| 0 | 100-125ms |
| 1 | 200-250ms |
| 2 | 400-500ms |
| 3 | 800-1000ms |
| 4+ | 2000-2500ms |

---

## Once method

`Dispatcher.Once` la wrapper non-streaming cua `Stream`. No thu thap tat ca cac `StreamEvent` tu channel roi aggregate thanh mot `FullResponse` duy nhat:

```go
type FullResponse struct {
    Text         string
    Thinking     string
    ToolUses     []ToolUseEntry
    Usage        Usage
    ContextUsage *ContextUsage
    StopReason   string
}
```

Cac `TextDelta` duoc noi chuoi vao `Text`. `ThinkingDelta` vao `Thinking`. `ToolUseStart/Delta/Stop` duoc reconstruct thanh cac `ToolUseEntry` hoan chinh. `Usage` va `ContextUsage` duoc giu lai tu event cuoi.

Neu gap `ErrorEvent` trong stream, `Once` tra ve loi ngay lap tuc voi bat ky partial response nao da thu thap duoc.
