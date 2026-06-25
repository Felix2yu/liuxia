## 朝霞晚霞预警脚本程序

用户可通过环境变量配置每天查询火烧云的时间和预期质量，最后通过 ntfy 推送信息。

由 sunsetbot.top 提供接口。Docker 镜像支持 `linux/amd64` 和 `linux/arm64` 架构。

## 功能特性

- 多城市监控支持
- 深色/浅色主题切换
- 数据统计与图表分析
- 城市间数据对比
- API 响应缓存
- Prometheus 监控指标
- 数据自动清理策略
- 移动端响应式设计

## 部署

推荐使用 Docker Compose 部署：

```yaml
services:
  sunsetbot:
    container_name: sunsetbot
    image: ghcr.io/felix2yu/sunsetbot:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
    environment:
      - CITY=江苏省-苏州
      - CITIES=江苏省-苏州,上海市-上海
      - BASE_URL=https://sunsetbot.top/
      - PUSH_ENABLE=true
      - NTFY_SERVER=https://ntfy.sh
      - NTFY_TOPIC=Weather
      - NTFY_TOKEN=
      - SEND_TEST_ON_START=false
      - PUSH_ERROR=true
      - MORNING_ENABLE=true
      - MORNING_TIME=18:00,00:00
      - MORNING_MODEL=GFS,EC
      - EVENING_ENABLE=true
      - EVENING_TIME=08:00,11:30,16:00
      - EVENING_MODEL=GFS,EC
      - DB_PATH=/app/data/sunset.db
      - WEB_PORT=8080
      - DATA_RETENTION_DAYS=365
      - PUID=1000
      - PGID=1000
    restart: unless-stopped
```

## 环境变量

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `CITY` | 是 | — | 城市，如 `江苏省-苏州` |
| `CITIES` | 否 | 空 | 逗号分隔的多城市列表，如 `江苏省-苏州,上海市-上海` |
| `NTFY_TOPIC` | 是 | — | ntfy 主题 |
| `BASE_URL` | 否 | `https://sunsetbot.top/` | 服务基础 URL |
| `PUSH_ENABLE` | 否 | `true` | 是否启用推送 |
| `NTFY_SERVER` | 否 | `https://ntfy.sh` | ntfy 服务器地址 |
| `NTFY_TOKEN` | 否 | 空 | ntfy 认证 token（可选） |
| `SEND_TEST_ON_START` | 否 | `false` | 启动时推送测试消息 |
| `PUSH_ERROR` | 否 | `true` | 请求错误时推送 |
| `MORNING_ENABLE` | 否 | `true` | 朝霞任务是否启用 |
| `MORNING_TIME` | 否 | `18:00,00:00` | 朝霞执行时间，逗号分隔 |
| `MORNING_MODEL` | 否 | `GFS,EC` | 朝霞模型，逗号分隔 |
| `EVENING_ENABLE` | 否 | `true` | 晚霞任务是否启用 |
| `EVENING_TIME` | 否 | `08:00,11:30,16:00` | 晚霞执行时间，逗号分隔 |
| `EVENING_MODEL` | 否 | `GFS,EC` | 晚霞模型，逗号分隔 |
| `DB_PATH` | 否 | `sunset.db` | SQLite 数据库文件路径 |
| `WEB_PORT` | 否 | `8080` | Web 看板端口 |
| `DATA_RETENTION_DAYS` | 否 | `365` | 数据保留天数，设为 `0` 禁用自动清理 |
| `PUID` | 否 | `1000` | 容器运行用户 UID |
| `PGID` | 否 | `1000` | 容器运行用户 GID |
| `TZ` | 否 | `Asia/Shanghai` | 时区 |

## 消息推送

使用 ntfy 推送信息，也可自建部署本地服务。

官方 ntfy 地址：<https://ntfy.sh/>

页面上新建 Topic 后填入环境变量 `NTFY_TOPIC`；如需使用需要验证身份的 Topic，可通过 `NTFY_TOKEN` 传入认证 token。

### 通知等级

Ntfy 通知等级对应关系：

- 过滤阈值：< 0.2 的数据会被过滤掉，不通知
- 0.2 - 0.4 → 等级 1
- 0.4 - 0.6 → 等级 2
- 0.6 - 0.8 → 等级 3
- 0.8 - 1.0 → 等级 4
- 1.0 及以上 → 等级 5

ntfy 消息中质量、气溶胶数值较优秀时会加粗标记。

![](.img/snapshot.jpg)

## 数据看板

启动后访问 `http://localhost:8080` 查看历史数据看板，支持：

- 多城市筛选
- 按朝霞/晚霞筛选
- 按日期范围查询
- 快捷时间范围（近1月/3月/半年/1年/全部）
- 鲜艳度和气溶胶折线图（按模型分组）
- 模型对比统计图表
- 月度趋势图表
- 城市对比图表
- 图表导出为 PNG
- 深色/浅色主题切换
- 表格分页（每页20条）
- 导出 CSV / JSON
- 移动端响应式设计

每次获取数据后自动写入 SQLite 数据库（`sunset.db`），已存在数据自动更新。

## API 接口

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/data` | GET | 查询历史数据 |
| `/api/statistics` | GET | 获取统计数据 |
| `/api/cities` | GET | 获取城市列表 |
| `/api/city-comparison` | GET | 城市间数据对比 |
| `/api/export` | GET | 导出数据（CSV/JSON） |
| `/api/health` | GET | 健康检查 |
| `/metrics` | GET | Prometheus 指标 |

### 查询参数

| 参数 | 说明 | 示例 |
|------|------|------|
| `city` | 城市筛选 | `江苏省-苏州` |
| `event_type` | 事件类型 | `morning` 或 `evening` |
| `start` | 开始日期 | `2024-01-01` |
| `end` | 结束日期 | `2024-12-31` |
| `format` | 导出格式 | `csv` 或 `json` |

### 响应头

| 头 | 说明 |
|------|------|
| `X-Cache` | 缓存命中状态：`HIT` 或 `MISS` |
