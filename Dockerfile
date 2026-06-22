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
FROM alpine:3.20

WORKDIR /app

RUN apk add --no-cache tzdata ca-certificates

ENV TZ=Asia/Shanghai

COPY --from=builder /build/sunsetbot /app/sunsetbot

CMD ["/app/sunsetbot"]
