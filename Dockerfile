# 多阶段构建：第一阶段 - 构建阶段
FROM golang:1.26-alpine AS builder

WORKDIR /build

# 设置 Go 环境变量
ENV CGO_ENABLED=0 \
    GOOS=linux

# 复制依赖文件
COPY go.mod go.sum ./

# 下载依赖
RUN go mod download

# 复制代码并构建
COPY . .

# 构建二进制文件
RUN go build -ldflags="-s -w" -o /build/sunsetbot .

# 第二阶段 - 运行阶段
FROM alpine:latest

WORKDIR /app

RUN apk add --no-cache tzdata ca-certificates gosu \
    && addgroup -g 1000 appuser \
    && adduser -D -u 1000 -G appuser appuser

COPY --from=builder /build/sunsetbot /app/sunsetbot
COPY templates /app/templates
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

ENV TZ=Asia/Shanghai \
    PUID=1000 \
    PGID=1000

EXPOSE 8080

ENTRYPOINT ["/app/entrypoint.sh"]
