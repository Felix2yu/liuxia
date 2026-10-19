# 运行时镜像：二进制由 CI 矩阵预编译并下载到 bin/ 后拼装。
#
# 基镜 ghcr.io/felix2yu/base:alpine-3.24 提供 tzdata、ca-certificates 与 PUID/PGID
# 降权入口；原先本仓库用 gosu + appuser，现由基镜统一为 su-exec + 数字 uid/gid，
# 效果一致（真正决定权限的是 chown 的数字属主）。契约见 Felix2yu/.github/docs/docker.md。
FROM ghcr.io/felix2yu/base:alpine-3.24

WORKDIR /app

# 本仓库的容器布局：二进制在 /app，数据在 /app/data。
# /app 不在基镜的 PATH 上，所以启动命令与外部备份工具都用绝对路径
# （CMD ["/app/liuxia"]、BACKUP_SQLITE_CMD=/app/liuxia backup）。
# WORKDIR 是 /app，相对路径的默认值不受影响。
COPY --chmod=755 bin/liuxia /app/liuxia

# 注意：static/ 与 templates/ 已在编译期通过 go:embed 打入二进制，镜像无需再 COPY 它们。
# APP_DATA_DIR 只指数据目录：基镜入口会 chown 它，写成 /app 会连二进制一起改属主。
ENV TZ=Asia/Shanghai \
    APP_DATA_DIR=/app/data

EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD wget -qO- http://127.0.0.1:8080/api/health >/dev/null || exit 1

CMD ["/app/liuxia"]
