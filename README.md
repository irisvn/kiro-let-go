# kiro-let-go

## Đây là gì?

Một cổng kết nối (gateway proxy) viết bằng Go đóng vai trò là proxy đứng trước nhiều tài khoản Kiro AI. Nó cung cấp các API tương thích hoàn toàn với định dạng của OpenAI và Anthropic. Hệ thống hỗ trợ tự động failover (chuyển đổi dự phòng khi tài khoản lỗi), các cơ chế bảo vệ chống khóa tài khoản (anti-ban), tự động kiểm tra quota và cung cấp giao diện quản trị Admin UI trực quan. 

Proxy này tự động cân bằng tải các request qua các tài khoản, tự động khôi phục khi gặp sự cố và giả lập lưu lượng giống như IDE thực tế để tránh cơ chế phát hiện ban của Kiro. Nhờ đó, bạn có thể tích hợp trực tiếp vào các client AI hiện tại (như OpenCode, Cursor, v.v.) mà không cần thay đổi mã nguồn.

---

## Biên dịch

Chạy lệnh sau tại thư mục gốc để biên dịch dự án:

```bash
make build
```

Lệnh này sẽ tự động biên dịch bundle frontend React của Admin UI, nhúng vào mã nguồn Go và tạo ra hai tệp thực thi trong thư mục `bin/`:

- `kiro-let-go` — HTTP Server (Proxy & Admin API/UI)
- `kiro-let-go-cli` — Công cụ dòng lệnh CLI để quản trị hệ thống

---

## Cấu hình ban đầu

1. Sao chép tệp cấu hình mẫu và chỉnh sửa:

```bash
cp configs/config.example.json configs/config.json
```

2. Thay thế các khóa API mặc định:

- `server.admin_api_key` — Khóa dùng để quản trị tài khoản qua REST API và CLI
- `server.proxy_api_key` — Khóa API mà client sử dụng khi gửi chat request lên proxy

3. (Tùy chọn) Đặt cấu hình `storage.credentials_json_path` nếu bạn muốn tự động nạp tài khoản từ một tệp JSON trên đĩa. Máy chủ sẽ giám sát tệp đó và tự động đồng bộ hóa các thay đổi.

---

## Thêm tài khoản Kiro

Bạn cần thêm ít nhất một tài khoản Kiro để proxy có thể xử lý các yêu cầu. Có ba cách để thêm tài khoản:

### 1. Sử dụng công cụ CLI

```bash
# Thêm tài khoản mạng xã hội (Social Auth)
./bin/kiro-let-go-cli account add \
  --type social \
  --label my-account \
  --refresh-token "<your-refresh-token>" \
  --region us-east-1

# Thêm tài khoản sử dụng API Key
./bin/kiro-let-go-cli account add \
  --type apikey \
  --label my-apikey-account \
  --key "ksk_xxxxxxxxxxxxxxxxxxxxxx" \
  --region us-east-1
```

### 2. Sử dụng REST API

```bash
curl -X POST http://localhost:8765/admin/accounts \
  -H "Authorization: Bearer REPLACE_ME_ADMIN" \
  -H "Content-Type: application/json" \
  -d '{
    "label": "my-account",
    "auth_method": "social",
    "refresh_token": "<your-refresh-token>",
    "region": "us-east-1",
    "enabled": true
  }'
```

*(Đối với phương thức API Key, hãy đổi `"auth_method"` thành `"apikey"` và truyền `"api_key": "ksk_..."` thay vì `refresh_token`)*

### 3. Sử dụng tệp cấu hình JSON

Tạo một tệp cấu hình JSON chứa danh sách tài khoản (ví dụ: `configs/credentials.json`) và trỏ cấu hình `storage.credentials_json_path` tới tệp này:

```json
[
  {"label":"social-acct","auth_method":"social","refresh_token":"<token>","profile_arn":"<arn>","region":"us-east-1","enabled":true},
  {"label":"apikey-acct","auth_method":"apikey","api_key":"ksk_xxxxxxxxxxxxxxxxxxxxxx","enabled":true}
]
```

Máy chủ sẽ giám sát tệp này và tự động đồng bộ hóa tài khoản. Bạn cũng có thể thêm thuộc tính `"_delete": true` vào một mục để xóa tài khoản đó, hoặc thêm một mục đặc biệt `{"_remove_unlisted": true}` để tự động xóa bất kỳ tài khoản nào trong hệ thống không có tên trong tệp này.

---

## Cách sử dụng

Cấu hình cho ứng dụng của bạn (ví dụ OpenAI hoặc Anthropic SDK) trỏ endpoint về `http://localhost:8765` và sử dụng giá trị `server.proxy_api_key` của bạn làm API Key.

### Endpoint tương thích Anthropic

```bash
curl http://localhost:8765/v1/messages \
  -H "Authorization: Bearer REPLACE_ME_PROXY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Xin chào"}],
    "max_tokens": 1024
  }'
```

### Endpoint tương thích OpenAI

```bash
curl http://localhost:8765/v1/chat/completions \
  -H "Authorization: Bearer REPLACE_ME_PROXY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-sonnet-4.6",
    "messages": [{"role": "user", "content": "Xin chào"}]
  }'
```

> [!NOTE]
> Các mô hình được hỗ trợ bao gồm `claude-sonnet-4.5`, `claude-sonnet-4.6`, `claude-opus-4.5`, `claude-opus-4.6`, `claude-opus-4.7`, và `claude-haiku-4.5`. Bạn cũng có thể sử dụng các tên gọi tắt: `sonnet`, `opus`, và `haiku`.
> Chế độ stream (truyền tải dữ liệu thời gian thực) được hỗ trợ trên cả hai endpoint. Chỉ cần thêm `"stream": true` vào thân request.

---

## Kiểm tra Quota

### Qua công cụ CLI

```bash
# Xem tổng hợp hạn mức của tất cả các tài khoản
./bin/kiro-let-go-cli quota

# Bắt buộc cập nhật hạn mức trực tiếp cho một tài khoản cụ thể
./bin/kiro-let-go-cli quota <account-id> --force
```

### Qua REST API

```bash
# Xem tổng hợp hạn mức của tất cả các tài khoản
curl http://localhost:8765/admin/quota \
  -H "Authorization: Bearer REPLACE_ME_ADMIN"

# Lấy hạn mức của một tài khoản cụ thể
curl "http://localhost:8765/admin/accounts/<account-id>/quota?force=true" \
  -H "Authorization: Bearer REPLACE_ME_ADMIN"
```

---

## Các tính năng nâng cao (Cấu hình động)

Hệ thống tích hợp bảng cài đặt động trong Admin UI (`http://localhost:8765/admin/ui/settings`), cho phép thay đổi cấu hình nóng và áp dụng ngay lập tức mà không cần khởi động lại máy chủ:

* **Bật/Tắt Ghi Request Logs**:
  Cho phép tắt hoàn toàn việc lưu trữ request logs vào bộ nhớ hoặc ghi xuống tệp `.data/request_log.jsonl`. Khi tắt, hệ thống sẽ thực sự bỏ qua tất cả logic của middleware log, giúp giải phóng hoàn toàn tài nguyên ổ đĩa và cải thiện hiệu năng xử lý.
* **Extended Thinking (Giả lập Thinking Mode)**:
  Bật chế độ giả lập suy nghĩ mở rộng (`<thinking>...</thinking>` tags) của AI trước khi trả lời, giúp mô phỏng trải nghiệm suy nghĩ sâu và cải thiện chất lượng phản hồi từ các dòng mô hình lớn.
* **MCP Web Search**:
  Cho phép AI tự động thực hiện tìm kiếm thông tin thời gian thực từ Internet thông qua các máy chủ MCP API đã được cấu hình.
* **Truncation Recovery (Tự động phục hồi cắt ngắn)**:
  Tự động phát hiện và cảnh báo khi phản hồi từ máy chủ upstream bị cắt ngắn do vượt quá giới hạn token đầu ra.

---

## Chống khóa tài khoản (Anti-ban Notes)

Để bảo vệ các tài khoản Kiro, hệ thống thực hiện giả lập hoàn hảo lưu lượng truy cập từ IDE chính thức:

- **Mã máy riêng biệt cho từng tài khoản (Per-account Machine ID):** Mỗi tài khoản được cấp một ID máy cố định và duy nhất dựa trên nhãn (label). Upstream sẽ thấy các tài khoản như thể đang chạy từ các máy tính hoàn toàn khác nhau.
- **Fingerprint nhất quán:** Các chuỗi phiên bản, hệ điều hành (OS), và User-Agent được chọn ngẫu nhiên nhưng nhất quán cho mỗi tài khoản, đảm bảo vân tay trình duyệt không bị thay đổi giữa các request.
- **Cơ chế Circuit Breaker (Cầu dao tự động):** Nếu một tài khoản bị lỗi liên tục, nó sẽ tạm thời được đưa ra ngoài vòng xoay. Một tỷ lệ phần trăm request rất nhỏ sẽ được gửi thử nghiệm định kỳ để kiểm tra xem tài khoản đã tự phục hồi hay chưa.
- **Cách ly Proxy (Proxy Isolation):** Mỗi tài khoản có thể định tuyến qua một HTTP hoặc SOCKS5 proxy riêng biệt, độc lập hoàn toàn các kết nối mạng.
- **Sticky Sessions:** Khi được bật, các yêu cầu liên tiếp trong cùng một hội thoại (conversation) sẽ ưu tiên sử dụng cùng một tài khoản để tránh việc đổi danh tính liên tục trong một phiên chat.

---

## Tham chiếu cấu hình tĩnh (`configs/config.json`)

| Phân vùng | Trường cấu hình | Mặc định | Mô tả |
|---------|-------|---------|-------------|
| `server` | `host` | `0.0.0.0` | Địa chỉ bind cho HTTP server |
| `server` | `port` | `8765` | Cổng lắng nghe |
| `server` | `admin_api_key` | *(Bắt buộc)* | Token dùng cho các API quản trị và CLI |
| `server` | `proxy_api_key` | *(Bắt buộc)* | API key mà client sử dụng để gửi chat |
| `kiro` | `region` | `us-east-1` | Vùng Kiro mặc định |
| `storage` | `sqlite_path` | `.data/kiro.db` | Đường dẫn lưu file cơ sở dữ liệu SQLite |
| `storage` | `credentials_json_path` | ` ""` | Đường dẫn tệp JSON chứa tài khoản mà máy chủ giám sát |
| `logging` | `level` | `info` | Cấp độ log của server: `debug`, `info`, `warn`, `error` |
| `logging` | `format` | `json` | Định dạng log: `json` hoặc `text` |

*(Lưu ý: Tất cả các cài đặt tĩnh có thể được ghi đè bằng các biến môi trường có tiền tố `KIRO_`, ví dụ: `KIRO_SERVER_PORT=8080`)*

---

## Hạn chế hiện tại

Các tính năng sau hiện chưa được hỗ trợ:

- **Các mô hình ngoài họ Claude:** Chỉ các mô hình thuộc dòng Claude của Kiro hoạt động. GPT, Gemini, v.v. không khả dụng.
- **URL hình ảnh dạng HTTP/HTTPS:** Hình ảnh tải lên bắt buộc phải được mã hóa dưới dạng dữ liệu base64 URL. Các liên kết hình ảnh trực tiếp từ internet sẽ bị từ chối.
- **Cơ chế đẩy cập nhật quota thời gian thực:** Quota chỉ được cập nhật định kỳ hoặc bắt buộc cập nhật thủ công; không có cơ chế Webhook hay Server-Sent Events tự động từ Kiro.
- **Hội thoại xuyên suốt giữa các tài khoản:** Proxy hoàn toàn phi trạng thái (stateless). Trạng thái và lịch sử hội thoại phải được quản lý trực tiếp bởi client.
