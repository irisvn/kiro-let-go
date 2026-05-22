# Configuration

## Cấu trúc Config struct

Cấu hình ứng dụng được định nghĩa trong `internal/config/config.go`.

| Field | Type | Default | Mô tả |
|-------|------|---------|-------|
| `Server.Host` | `string` | `"0.0.0.0"` | Địa chỉ bind của HTTP server |
| `Server.Port` | `int` | `8765` | Cổng lắng nghe |
| `Server.AdminAPIKey` | `string` | *(bắt buộc)* | Bearer token cho admin endpoints |
| `Server.ProxyAPIKey` | `string` | *(bắt buộc)* | API key cho client chat endpoints |
| `Kiro.Region` | `string` | `"us-east-1"` | Region mặc định cho Kiro |
| `Kiro.AuthRegion` | `string` | `"us-east-1"` | Region dùng cho authentication requests |
| `Kiro.APIRegion` | `string` | `"us-east-1"` | Region dùng cho API requests |
| `Storage.SQLitePath` | `string` | `".data/kiro.db"` | Đường dẫn file SQLite |
| `Storage.CredentialsJSONPath` | `string` | `""` | Đường dẫn file JSON để watcher sync accounts |
| `LoadBalancer.Strategy` | `string` | `"round_robin"` | Chiến lược chọn account (hiện chỉ hỗ trợ `round_robin`) |
| `LoadBalancer.StickySession` | `bool` | `true` | Giữ cùng một account cho các request liên tiếp cùng conversation |
| `Quota.CacheTTLSeconds` | `int` | `43200` | Thời gian cache quota (12 giờ) |
| `Failover.BaseCooldownSec` | `int` | `60` | Thời gian cooldown ban đầu của circuit breaker |
| `Failover.MaxBackoffMultiplier` | `int` | `1440` | Hệ số backoff tối đa cho circuit breaker |
| `Failover.ProbabilisticRetryChance` | `float64` | `0.10` | Xác suất thử lại account đang mở circuit (0..1) |
| `Failover.MaxAttempts` | `int` | `9` | Số lần retry tối đa của dispatcher cho mỗi upstream request |
| `Logging.Level` | `string` | `"info"` | Mức log: `debug`, `info`, `warn`, `error` |
| `Logging.Format` | `string` | `"json"` | Định dạng log: `json` hoặc `text` |

## Layered loading order

Config được load theo thứ tự ưu tiên từ thấp đến cao. Giá trị ở lớp sau sẽ ghi đè lớp trước:

1. **Defaults** — `setDefaults(v)` trong `config.go` gán giá trị mặc định cho tất cả fields.
2. **JSON file** — Nếu truyền `--config <path>`, viper đọc file JSON qua `v.SetConfigFile(path)` và `v.ReadInConfig()`.
3. **Environment variables** — Prefix `KIRO_`, viper tự động quét biến môi trường qua `v.AutomaticEnv()`.
4. **CLI flags** — Các flag từ `pflag.FlagSet` được bind vào viper qua `v.BindPFlags(flags)`.

Đoạn code cốt lõi:

```go
func LoadWithFlags(path string, flags *pflag.FlagSet) (*Config, error) {
    v := viper.New()
    setDefaults(v)                 // 1. defaults
    if path != "" {
        v.SetConfigFile(path)
        _ = v.ReadInConfig()       // 2. JSON file
    }
    v.SetEnvPrefix("KIRO")
    v.AutomaticEnv()               // 3. env vars
    bindEnvs(v, "", reflect.TypeOf(Config{}))
    if flags != nil {
        _ = v.BindPFlags(flags)    // 4. CLI flags
    }
    var cfg Config
    if err := v.Unmarshal(&cfg); err != nil {
        return nil, err
    }
    return &cfg, nil
}
```

## Environment variables

Tất cả fields trong `Config` đều có thể set qua biến môi trường với quy tắc:

- Prefix: `KIRO_`
- Dấu chấm (`.`) trong struct path thay bằng dấu gạch dưới (`_`)
- Viết hoa toàn bộ

Ví dụ:

| Config field | Biến môi trường |
|--------------|-----------------|
| `server.port` | `KIRO_SERVER_PORT` |
| `server.admin_api_key` | `KIRO_SERVER_ADMIN_API_KEY` |
| `kiro.region` | `KIRO_KIRO_REGION` |
| `storage.sqlite_path` | `KIRO_STORAGE_SQLITE_PATH` |
| `load_balancer.sticky_session` | `KIRO_LOAD_BALANCER_STICKY_SESSION` |
| `failover.max_attempts` | `KIRO_FAILOVER_MAX_ATTEMPTS` |

Server binary cũng hỗ trợ truyền trực tiếp flag dạng dot-notation:

```bash
./bin/kiro-let-go --server.port=8080 --logging.level=debug
```

## Validation rules

`Config.Validate()` kiểm tra hai điều kiện bắt buộc:

- `Server.AdminAPIKey` không được rỗng.
- `Server.ProxyAPIKey` không được rỗng.

Nếu thiếu, server sẽ thoát ngay ở startup với exit code 1 và message:

```
error: validate config: Server.AdminAPIKey is required
```

Không có validation nào khác được thực hiện trong `Validate()`. Các giá trị như region, strategy, log level được tin tưởng đúng định dạng và được xử lý ở runtime.

## Example configs

File cấu hình mẫu tĩnh (Tối giản): [configs/config.example.json](../../configs/config.example.json)

```json
{
  "server": {
    "host": "0.0.0.0",
    "port": 8765,
    "admin_api_key": "REPLACE_ME_ADMIN",
    "proxy_api_key": "REPLACE_ME_PROXY"
  },
  "kiro": {
    "region": "us-east-1",
    "auth_region": "us-east-1",
    "api_region": "us-east-1"
  },
  "storage": {
    "sqlite_path": ".data/kiro.db",
    "credentials_json_path": ""
  },
  "logging": {
    "level": "info",
    "format": "json"
  }
}
```

File credentials mẫu dùng cho watcher: [configs/credentials.example.json](../../configs/credentials.example.json)

```json
[
  {"label":"social-acct","auth_method":"social","refresh_token":"<your-kiro-refresh-token>","profile_arn":"<your-arn>","region":"us-east-1","enabled":true},
  {"label":"apikey-acct","auth_method":"apikey","api_key":"ksk_xxxxxxxxxxxxxxxxxxxxxx","enabled":true}
]
```

## Cấu hình động (Dynamic Config - SQLite DB, Hot-Reload)

Để tăng tính linh hoạt và giảm thiểu rủi ro khi thay đổi cấu hình, toàn bộ các cấu hình hoạt động và suy luận được tách khỏi file JSON tĩnh và lưu trữ trong bảng `settings` của cơ sở dữ liệu SQLite dưới dạng key-value.

- **Khởi tạo (Seeding)**: Khi cơ sở dữ liệu trống, hệ thống sẽ tự động gieo dữ liệu mặc định an toàn thông qua hàm `SeedFromStatic(cfg)` tại thời điểm khởi chạy. Từ các lần sau, Database đóng vai trò là **Source of Truth** (Nguồn chân lý duy nhất).
- **Cơ chế Hot-Reload**: Các thành phần hệ thống đọc cấu hình mới nhất qua phương thức `DynamicConfig.Get()` ở mỗi request. Sử dụng khóa đọc/ghi `sync.RWMutex` giúp chi phí tài nguyên cực kỳ thấp, cập nhật tức thì mà không cần khởi động lại máy chủ.
- **Quản lý trực quan**: Hỗ trợ xem và cập nhật trực tiếp qua giao diện **Admin UI** (Tab Settings).

### Các trường cấu hình động chính:

| Nhóm | Khóa cấu hình | Kiểu dữ liệu | Mặc định | Mô tả |
|------|---------------|--------------|----------|-------|
| **Load Balancer** | `strategy` | `string` | `"round_robin"` | Chiến lược cân bằng tải: `round_robin`, `balanced`, hoặc `most_quota`. |
| | `sticky_session` | `bool` | `true` | Duy trì cùng một tài khoản Kiro cho các lượt gửi tiếp theo trong cùng hội thoại. |
| **Circuit Breaker** | `base_cooldown_sec` | `int` | `60` | Thời gian hồi chiêu ban đầu (giây) của tài khoản khi lỗi. |
| | `max_backoff_multiplier` | `int` | `1440` | Hệ số nhân hồi chiêu tối đa. |
| | `probabilistic_retry_chance` | `float64` | `0.10` | Xác suất thử lại tài khoản đang hồi chiêu (0.0 đến 1.0). |
| | `max_attempts` | `int` | `9` | Số lần thử lại tối đa cho mỗi lượt gửi trước khi báo lỗi. |
| **Inference (Suy luận)**| `web_search_enabled` | `bool` | `false` | Bật/tắt tính năng Web Search của Kiro. |
| | `first_token_timeout_sec` | `int` | `15` | Giới hạn thời gian (giây) nhận token đầu tiên. |
| | `first_token_max_retries` | `int` | `3` | Số lần thử lại tối đa khi quá thời gian nhận token đầu tiên. |
| | `streaming_read_timeout_sec` | `int` | `300` | Giới hạn thời gian (giây) đọc luồng stream. |
| | `truncation_recovery_enabled` | `bool` | `true` | Tự động phục hồi khi hội thoại bị cắt bớt do quá giới hạn context. |
| | `fake_reasoning_enabled` | `bool` | `true` | Mô phỏng suy luận sâu (Fake Reasoning) bằng cách chèn thẻ suy nghĩ. |
| | `fake_reasoning_max_tokens` | `int` | `1024` | Giới hạn token tối đa cho phần suy nghĩ mô phỏng. |
| | `fake_reasoning_budget_cap` | `int` | `0` | Giới hạn ngân sách token tối đa cho phần suy nghĩ. |
| **Quota Cache** | `cache_ttl_seconds` | `int` | `43200` | Thời gian lưu cache hạn ngạch (giây). |
| **Mappings** | `model_mappings` | `json` | `[]` | Mảng chứa các quy tắc ánh xạ model từ client sang model Kiro. |

---

## Trình tạo cấu hình OpenCode (OpenCode Config Generator)

Để hỗ trợ OpenCode Agent tích hợp tối ưu và tránh lỗi lặp vòng lặp suy luận, Admin UI cung cấp tab **OpenCode Config**:
- Tự động phát hiện và sinh tệp cấu hình `config.json` tiêu chuẩn cho OpenCode Agent.
- Widget sao chép model thông minh, loại bỏ các thuộc tính không cần thiết (như `"family"`) tránh xung đột.
- Hướng dẫn cài đặt tệp cấu hình chi tiết cho các hệ điều hành (macOS, Linux, Windows).

---

## Cấu trúc Quy tắc Ánh xạ Model (Model Mappings)

```json
{
  "id": "gpt4-to-sonnet",
  "name": "GPT-4 → Sonnet",
  "enabled": true,
  "rule_type": "replace",
  "source_model": "gpt-4",
  "target_models": ["claude-sonnet-4.6"],
  "weights": []
}
```

Các kiểu quy tắc (`rule_type`):
- `replace` — Ánh xạ 1:1, thay thế hoàn toàn model nguồn bằng model đích.
- `alias` — Giữ nguyên model, coi tên nguồn và đích là bí danh của nhau.
- `loadbalance` — Phân phối tải yêu cầu giữa nhiều model đích theo tỷ lệ trọng số (`weights`).
```
