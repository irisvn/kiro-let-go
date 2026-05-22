# Stage 1: Build frontend React
FROM node:20-alpine AS frontend-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Build Go backend
FROM golang:alpine AS backend-builder
ENV GOTOOLCHAIN=auto
WORKDIR /app

# Khai báo build argument cho Version (mặc định là prod nếu không truyền)
ARG VERSION=prod

COPY go.mod go.sum ./
RUN go mod download

# Copy mã nguồn backend
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Sao chép tệp bundle frontend đã build từ stage 1 vào đúng vị trí nhúng tĩnh
COPY --from=frontend-builder /app/internal/api/adminui/dist/ ./internal/api/adminui/dist/

# Biên dịch tĩnh các binaries Go không sử dụng CGO (giúp chạy trên môi trường tối giản an toàn)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w -X github.com/irisvn/kiro-let-go/internal/version.Version=${VERSION}" -o bin/kiro-let-go ./cmd/server && \
    CGO_ENABLED=0 GOOS=linux go build -ldflags "-s -w -X github.com/irisvn/kiro-let-go/internal/version.Version=${VERSION}" -o bin/kiro-let-go-cli ./cmd/cli

# Stage 3: Runner
FROM alpine:3.19
RUN apk add --no-cache ca-certificates tzdata sqlite

WORKDIR /app

# Sao chép tệp thực thi biên dịch tĩnh từ stage 2
COPY --from=backend-builder /app/bin/kiro-let-go /app/kiro-let-go
COPY --from=backend-builder /app/bin/kiro-let-go-cli /app/kiro-let-go-cli

# Cấu hình các biến môi trường mặc định có tiền tố KIRO_
ENV KIRO_SERVER_HOST=0.0.0.0
ENV KIRO_SERVER_PORT=8765
ENV KIRO_STORAGE_SQLITE_PATH=/app/.data/kiro.db
ENV KIRO_LOGGING_REQUEST_LOG_FILE=/app/.data/request_log.jsonl

# Tạo thư mục dữ liệu .data cho SQLite và Logs
RUN mkdir -p /app/.data

# Mở cổng dịch vụ mặc định
EXPOSE 8765

# Khởi chạy trực tiếp server Go (Viper sẽ tự động đọc cấu hình mặc định và ghi đè từ ENV)
CMD ["/app/kiro-let-go"]
