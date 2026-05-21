# Tài liệu kiro-let-go

## Tổng quan dự án

`kiro-let-go` là một gateway proxy viết bằng Go, đóng vai trò trung gian giữa client và nhiều tài khoản Kiro AI. Dự án cung cấp các API tương thích với Anthropic Messages và OpenAI Chat Completions, hỗ trợ cả streaming lẫn non-streaming. Hệ thống tự động cân bằng tải giữa các tài khoản, chuyển đổi dự phòng khi gặp lỗi, tích hợp các kỹ thuật chống phát hiện (anti-ban), và cung cấp các tính năng reliability như auto-recovery, network error isolation, và circuit breaker cải tiến để giữ cho các tài khoản hoạt động ổn định.

## Kiến trúc tổng thể

Xem chi tiết tại [architecture.md](architecture.md). Kiến trúc gồm 7 lớp chính: HTTP layer dựa trên gin, API layer cho Anthropic/OpenAI, Conversion layer chuyển đổi request/response, Orchestration layer qua `kiro.Dispatcher`, Account layer quản lý tài khoản và circuit breaker, Protocol layer giao tiếp với AWS Event Stream của Kiro, và Storage layer dùng SQLite.

## Danh sách components

- [Architecture](architecture.md) — Kiến trúc tổng thể, luồng request, dependency graph và concurrency model.
- [HTTP Server](components/server.md) — Gin engine, middleware stack, routing, Request Log, và graceful shutdown.
- [CLI Tool](components/cli.md) — Công cụ dòng lệnh `kiro-let-go-cli` quản lý tài khoản, kiểm tra quota và khởi động server.
- [Configuration](components/configuration.md) — Hệ thống cấu hình đa lớp: defaults, JSON file, biến môi trường, CLI flags, và Settings (dynamic config) có thể thay đổi runtime từ Admin UI không cần restart.
- [Kiro Protocol Client](components/kiro-protocol.md) — Client giao tiếp với upstream Kiro, bao gồm auth refresh, header builder và AWS Event Stream decoder.
- [Account Management](components/account-management.md) — SQLite store, CRUD tài khoản, token refresh và machine ID.
- [Load Balancing & Failover](components/load-balancing.md) — Round-robin balancer, sticky session, circuit breaker, dispatcher retry, weighted model mapping với 3 rule types (replace, alias, loadbalance), và fallback chains.
- [Anti-Ban Techniques](components/anti-ban.md) — Per-account machine ID, header randomization, proxy isolation và health-probe avoidance.
- [Converters (Anthropic/OpenAI ↔ Kiro)](components/converters.md) — Chuyển đổi request và response giữa các định dạng Anthropic, OpenAI và Kiro protocol.
- [Streaming (SSE & Thinking parser)](components/streaming.md) — SSE writer cho Anthropic và OpenAI, FSM parser cho `<thinking>` tag.
- [Error Classification](components/errors.md) — Phân loại lỗi theo class: Fatal, Recoverable, QuotaExhausted, AuthExpired, RateLimited, Network.

## API Surfaces

- [Anthropic API Surface](api/anthropic.md) — Endpoint `POST /v1/messages`, hỗ trợ streaming qua SSE, tương thích Anthropic Messages API.
- [OpenAI API Surface](api/openai.md) — Endpoints `POST /v1/chat/completions` và `GET /v1/models`, tương thích OpenAI Chat Completions API.
- [Admin REST API](api/admin.md) — Các endpoint quản trị: CRUD tài khoản (`/admin/accounts`), kiểm tra quota (`/admin/quota`, `/admin/accounts/:id/quota`), test connection, chat test, model listing, và settings. Admin UI là React SPA được serve qua `embed.FS` tại `/admin/ui/`.

## Tính năng mới

- **Settings (dynamic config)**: xem [Configuration](components/configuration.md) — co the thay doi `strategy`, `sticky_session`, va cac setting khac runtime tu Admin UI.
- **Request Log**: ghi log chi tiet request/response cho cac endpoint proxy va test.
- **Reliability**: auto-recovery khi tat ca accounts bi block, network error isolation, circuit breaker voi threshold 3 failures.
- **Admin UI**: React SPA duoc serve qua `embed.FS`, thay the cho phien ban Alpine.js truoc day.
- **Model Mapping**: ho tro weighted rules (replace, alias, loadbalance) va fallback chains (vi du `opus-4.7 → opus-4.6 → opus-4.5 → sonnet-4.6`).

## Bắt đầu nhanh

Hướng dẫn build, cấu hình và chạy server có trong [README.md](../README.md) ở thư mục gốc của repo.
