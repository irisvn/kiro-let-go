# CLI Tool

## Mục đích

`kiro-let-go-cli` là công cụ dòng lệnh để quản lý tài khoản và kiểm tra quota mà không cần server đang chạy. CLI nối trực tiếp với file SQLite, nên mọi thay đổi đều có hiệu lực ngay lập tức trên cùng một database mà server sử dụng. Điều này hữu ích khi cần thêm tài khoản, vô hiệu hóa account bị lỗi, hoặc kiểm tra quota offline.

## Cấu trúc

CLI dựa trên thư viện `cobra`. Root command là `kiro-let-go-cli`.

Các persistent flags áp dụng cho mọi subcommand:

| Flag | Mặc định | Mô tả |
|------|----------|-------|
| `--config` | `configs/config.json` | Đường dẫn file cấu hình JSON |
| `--db` | "" | Ghi đè đường dẫn SQLite database |
| `--json` | `false` | Xuất output dạng JSON thay vì table |

PersistentPreRunE của root command gọi `init`, thực hiện các bước:

1. Load config từ file JSON.
2. Nếu `--db` được truyền, ghi đè `storage.sqlite_path`.
3. Kiểm tra file database tồn tại (trừ `:memory:`).
4. Mở SQLite connection, tạo store, khởi tạo manager, circuit breaker và quota fetcher.

Các subcommand được đăng ký dưới root:

```
kiro-let-go-cli
├── account
│   ├── add
│   ├── list
│   ├── get
│   ├── remove
│   ├── enable
│   ├── disable
│   └── refresh
├── quota
├── server
└── version
```

## Subcommands

| Subcommand | Arguments | Flags | Mô tả |
|-----------|-----------|-------|-------|
| `account add` | — | `--type` (required), `--label` (required), `--refresh-token`, `--key`, `--profile-arn`, `--region`, `--auth-region`, `--api-region`, `--proxy-url`, `--proxy-username`, `--proxy-password` | Thêm tài khoản mới. Với `social` auth sẽ thử refresh token ngay sau khi tạo. |
| `account list` | — | `--enabled-only`, `--auth-method` | Liệt kê tất cả accounts. Có thể lọc theo trạng thái enabled hoặc auth method. |
| `account get` | `<id>` | — | Hiển thị chi tiết một account, bao gồm trạng thái circuit breaker. |
| `account remove` | `<id>` | `--yes` | Xóa account khỏi database. Yêu cầu xác nhận trừ khi có `--yes`. |
| `account enable` | `<id>` | — | Kích hoạt lại account. |
| `account disable` | `<id>` | `--reason` | Vô hiệu hóa account, có thể kèm lý do. |
| `account refresh` | `<id>` | — | Buộc refresh token cho account (hữu ích khi token sắp hết hạn). |
| `quota` | `[account-id]` | `--force` | Hiển thị quota. Không có argument thì hiển thị summary tất cả accounts. `--force` bỏ qua cache. |
| `server` | — | — | Alias in ra thông báo: "Use kiro-let-go server instead". |
| `version` | — | — | In build version (git short SHA). |

## Examples

Thêm tài khoản social:

```bash
./bin/kiro-let-go-cli account add \
  --type social \
  --label my-social \
  --refresh-token "<token>" \
  --region us-east-1
```

Thêm tài khoản API key:

```bash
./bin/kiro-let-go-cli account add \
  --type apikey \
  --label my-apikey \
  --key "ksk_xxxxxxxxxxxxxxxxxxxxxx" \
  --region us-east-1
```

Liệt kê tất cả tài khoản:

```bash
./bin/kiro-let-go-cli account list
```

Xóa tài khoản (bỏ qua xác nhận):

```bash
./bin/kiro-let-go-cli account remove <account-id> --yes
```

Kiểm tra quota toàn bộ accounts (bypass cache):

```bash
./bin/kiro-let-go-cli quota --force
```

## Error handling

Mọi lỗi từ subcommand đều in ra `stderr` với prefix `[error]`:

```
[error] load config: open configs/config.json: no such file or directory
```

Exit code luôn là `1` khi có lỗi. Trong code, `cmd/cli/main.go` thực hiện điều này:

```go
if err := cli.Execute(); err != nil {
    _, _ = fmt.Fprintf(os.Stderr, "[error] %v\n", err)
    os.Exit(1)
}
```

## Output formats

Mặc định, CLI xuất bảng text qua `text/tabwriter`. Cột được căn tab, dễ đọc trên terminal.

Ví dụ output của `account list`:

```
ID                                    LABEL      AUTH    ENABLED  REGION     MACHINE_ID                            FAILURES  SUCCESSES  CREATED
xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx  my-social  social  true     us-east-1  <sha256-hex>                          0         5          2025-01-01T00:00:00Z
```

Khi truyền `--json`, toàn bộ output được serialize qua `json.NewEncoder` với indent 2 spaces. Điều này áp dụng cho mọi command trả về dữ liệu: `list`, `get`, `quota`.
