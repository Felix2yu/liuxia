# 流霞

> 流霞，天边流动的彩霞。古人以"流霞"喻仙酒、喻美景，今以此名，捕捉朝暮之间那一抹转瞬即逝的绚烂。

流霞是一款朝霞晚霞预报与监控工具。默认使用 Open-Meteo 预报与空气质量数据，在本地用规则模型计算 0–100 的「霞分」，同时保留历史鲜艳度字段，通过 [apprise-go](https://github.com/unraid/apprise-go) 推送提醒，并提供 Web 数据看板用于历史数据的可视化分析。

评分模型是 [霞 · 全球火烧云预报](https://github.com/baiiiii521/sunset-glow-forecast) `algo.js` 的 Go 纯函数移植（MIT），包含光路几何、光谱云底亮度、六级分档与多模式集合；阈值尚未经大规模样本标定，结果应作为观赏参考而非官方预报。旧的 sunsetbot.top 接口仍可通过 `DATA_SOURCE=sunsetbot` 使用。Docker 镜像支持 `linux/amd64` 和 `linux/arm64` 架构。

## 功能特性

- **定时预报** — 基于 GFS/EC 模型，按配置时间自动获取朝霞/晚霞预报
- **智能推送** — 通过 apprise-go 推送通知，按预报质量分 5 个等级，支持 Markdown 格式开关
- **数据看板** — 内置 Web 页面，支持折线图、模型对比、月度趋势、城市对比等可视化
- **最佳观赏排行** — 按历史数据排行 Top 10 高质量日期，季节对比分析
- **多城市监控** — 支持同时监控多个城市的朝霞/晚霞数据
- **数据持久化** — 自动写入 SQLite，支持 CSV/JSON 导出
- **Prometheus 指标** — 暴露 `/metrics` 端点，便于监控集成

## 部署

推荐使用 Docker Compose 部署：

```yaml
services:
  liuxia:
    container_name: liuxia
    image: ghcr.io/felix2yu/liuxia:latest
    ports:
      - "8080:8080"
    volumes:
      - ./data:/app/data
    environment:
      - CITY=江苏省-苏州,上海市-上海
      - DATA_SOURCE=open-meteo
      - PUSH_ENABLE=true
      - PUSH_MARKDOWN=true
      - PUSH_URL=ntfy://ntfy.sh/Weather,tgram://bot-token/chatid
      - SEND_TEST_ON_START=false
      - PUSH_ERROR=true
      - MORNING_ENABLE=true
      - MORNING_TIME=18:00,00:00
      - MORNING_MODEL=GFS,EC
      - EVENING_ENABLE=true
      - EVENING_TIME=08:00,11:30,16:00
      - EVENING_MODEL=GFS,EC
      - DB_PATH=/app/data/liuxia.db
      - BACKUP_SQLITE_CMD=/app/liuxia backup
      - WEB_PORT=8080
      - DATA_RETENTION_DAYS=365
      - PUID=1000
      - PGID=1000
    restart: unless-stopped
```

### 数据备份

数据是一份 SQLite 文件。运行中的库正在被写入，直接拷贝 `liuxia.db` 及其 `-wal`/`-shm`
旁文件不是原子操作，容易拷出损坏副本，所以由应用自己在事务里产出一致性快照：

```bash
docker exec liuxia /app/liuxia backup > liuxia-$(date +%F).db
```

`backup` 子命令把快照写到 stdout，日志走 stderr，因此重定向得到的就是可直接放回
数据目录使用的 `.db` 文件。恢复时把它放回 `./data/liuxia.db` 再启动容器即可。
[docker-db-auto-backup](https://github.com/Felix2yu/docker-db-auto-backup) 会按上面的
`BACKUP_SQLITE_CMD` 自动识别本容器并完成定时备份。

## 环境变量

### 推送配置

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `PUSH_ENABLE` | 否 | `true` | 是否启用推送 |
| `PUSH_MARKDOWN` | 否 | `true` | 是否使用 Markdown 格式推送 |
| `PUSH_URL` | 否* | — | apprise URL，支持多渠道逗号分隔（如 `ntfy://ntfy.sh/Weather,tgram://bot-token/chatid`） |
| `SEND_TEST_ON_START` | 否 | `false` | 启动时推送测试消息 |
| `PUSH_ERROR` | 否 | `true` | 请求错误时推送 |

> *启用推送时，需配置 `PUSH_URL`。

### 基础配置

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `CITY` | 是 | — | 城市，支持逗号分隔多城市，如 `江苏省-苏州` 或 `江苏省-苏州,上海市-上海` |
| `DATA_SOURCE` | 否 | `open-meteo` | 数据源：`open-meteo`（本地评分）或 `sunsetbot`（旧接口） |
| `BASE_URL` | 否 | `https://sunsetbot.top/` | 仅 `DATA_SOURCE=sunsetbot` 时使用 |
| `FORECAST_URL` | 否 | `https://api.open-meteo.com/v1/forecast` | Open-Meteo 预报接口，可指向自建或镜像服务 |
| `FORECAST_MODELS` | 否 | — | 强制指定预报模式，仅在指向集合接口等需要 `models` 参数的镜像时设置，如 `ecmwf_ifs025` |
| `AQ_URL` | 否 | `https://air-quality-api.open-meteo.com/v1/air-quality` | Open-Meteo 空气质量接口 |
| `GEOCODING_URL` | 否 | `https://geocoding-api.open-meteo.com/v1/search` | Open-Meteo 地理编码接口 |
| `DB_PATH` | 否 | `liuxia.db` | SQLite 数据库文件路径 |
| `BACKUP_SQLITE_CMD` | 否 | — | 设为 `/app/liuxia backup`（绝对路径：镜像把二进制放在 /app，不在 PATH 上），供外部备份工具识别并定时导出快照 |
| `WEB_PORT` | 否 | `8080` | Web 看板端口 |
| `DATA_RETENTION_DAYS` | 否 | `365` | 数据保留天数，设为 `0` 禁用自动清理 |
| `PUID` | 否 | `1000` | 容器运行用户 UID |
| `PGID` | 否 | `1000` | 容器运行用户 GID |
| `TZ` | 否 | `Asia/Shanghai` | 时区 |

若 `api.open-meteo.com` 在部署网络中不可达，可改用集合接口（需显式指定模式）：

```yaml
FORECAST_URL=https://ensemble-api.open-meteo.com/v1/ensemble
FORECAST_MODELS=ecmwf_ifs025
EVENING_MODEL=EC
MORNING_MODEL=EC
```

集合接口的 GFS 模式不提供低/中/高分层云量，此时关键云量视为缺失，霞分会被封顶。

### 任务配置

| 变量 | 必填 | 默认值 | 说明 |
|------|------|--------|------|
| `MORNING_ENABLE` | 否 | `true` | 朝霞任务是否启用 |
| `MORNING_TIME` | 否 | `18:00,00:00` | 朝霞执行时间，逗号分隔 |
| `MORNING_MODEL` | 否 | `GFS,EC` | 朝霞模型，逗号分隔 |
| `EVENING_ENABLE` | 否 | `true` | 晚霞任务是否启用 |
| `EVENING_TIME` | 否 | `08:00,11:30,16:00` | 晚霞执行时间，逗号分隔 |
| `EVENING_MODEL` | 否 | `GFS,EC` | 晚霞模型，逗号分隔 |

## 消息推送

使用 [apprise-go](https://github.com/unraid/apprise-go) Go 原生通知库，支持 100+ 通知渠道，无需额外部署，支持多渠道同时推送。

**配置环境变量：**

```yaml
PUSH_URL=ntfy://ntfy.sh/Weather
```

**多渠道同时推送：**

```yaml
PUSH_URL=ntfy://ntfy.sh/Weather,tgram://bot-token/chatid
```

**支持的 URL 格式：**

| 渠道 | URL 格式 |
|------|----------|
| ntfy | `ntfy://ntfy.sh/topic` |
| Telegram | `tgram://bot-token/chatid` |
| Slack | `slack://token-a/token-b/token-c` |
| Discord | `discord://token@channel` |
| 邮件 | `mail://smtp-server:port?username=xxx&password=xxx` |
| Pushover | `pover://token@userkey` |
| Gotify | `gotify://gotify-server/token` |

更多渠道请参考 [apprise-go 文档](https://github.com/unraid/apprise-go)。

### 通知等级

`DATA_SOURCE=open-meteo`（默认）按霞分推送，等级直接取模型的六级分档：

- 霞分 < 35（无烧 / 微烧）：只落库，不通知
- 35–49 小烧 → 等级 3
- 50–64 小到中烧 → 等级 4
- 65–79 中到大烧 → 等级 5
- ≥ 80 大烧 → 等级 5

`DATA_SOURCE=sunsetbot` 仍按旧鲜艳度分级：

- 过滤阈值：< 0.2 的数据会被过滤掉，不通知
- 0.2 - 0.4 → 等级 1
- 0.4 - 0.6 → 等级 2
- 0.6 - 0.8 → 等级 3
- 0.8 - 1.0 → 等级 4
- 1.0 及以上 → 等级 5

新数据写入 `score`/`level`/`grade`/`probability`/`clarity` 列，历史 `quality`/`aod` 原样保留，两种口径不混算。

设置 `PUSH_MARKDOWN=true` 时，消息中质量、气溶胶数值较优秀时会加粗标记；设置为 `false` 则发送纯文本。

![通知示例](.img/snapshot.jpg)

## 数据看板

启动后访问 `http://localhost:8080` 查看历史数据看板，支持：

- 多城市筛选
- 按朝霞/晚霞筛选
- 按日期范围查询
- 快捷时间范围（近1月/3月/半年/1年/全部）
- 趋势分面矩阵：按「现象 × 模型」拆成 4 张独立子图（朝霞/晚霞 × GFS/EC），支持双指标与单指标切换
- 模型对比：GFS vs EC 趋势对比与偏差分布（按现象分面）
- 月度趋势分面（每个组合单独成图）
- 季节对比分面（每个组合单独成图）
- 组合汇总对比图表
- 城市对比图表
- 最佳观赏排行（Top 10 高质量日期，按组合着色）
- 图表收起/展开（状态记忆）
- 图表导出为 PNG
- 深色/浅色主题切换（跟随系统）
- 表格分页（每页20条）
- 导出 CSV / JSON
- 移动端响应式设计

每次获取数据后自动写入 SQLite 数据库（`liuxia.db`），已存在数据自动更新。

## API 接口

| 端点 | 方法 | 说明 |
|------|------|------|
| `/api/data` | GET | 查询历史数据 |
| `/api/statistics` | GET | 获取统计数据 |
| `/api/rankings` | GET | 最佳观赏排行（Top 10 日期、月度、季节统计） |
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

## 离线评分评估

`cmd/glow-eval` 用 sunsetbot 公开的上海再分析标注（2022-06-24～2023-12-30，逐日日出/日落六档标签）离线检验 `internal/glow` 的评分，不改动线上取数与推送链路。

```bash
go run ./cmd/glow-eval                       # 输出到终端
go run ./cmd/glow-eval -out report.md        # 写入报告
go run ./cmd/glow-eval -refresh              # 忽略缓存重新抓取
go run ./cmd/glow-eval -limit 4              # 小样本冒烟
```

标注与历史气象缓存默认位于 `.cache/glow-eval/`，已由 `.gitignore` 忽略。历史输入来自 Open-Meteo Historical Forecast 与 Air Quality；该接口不提供历史降水概率，AOD 自 2022-08 中旬起才有值。上游点按周代表日方位角取 50–400 km，报告会明确列出这些口径限制。

### 标定实验

`calibrate` 子命令复用同一份缓存，输出候选参数与交叉验证指标，不修改 `internal/glow`、线上取数、存储与推送：

```bash
go run ./cmd/glow-eval calibrate                       # 输出到终端
go run ./cmd/glow-eval calibrate -out calibrate.md    # 写入报告
go run ./cmd/glow-eval calibrate -folds 5 -lambda 10  # 交叉验证折数与 L2 系数
```

报告包含三部分：六档阈值的动态规划最优解与按月交叉验证、概率 sigmoid 的逻辑回归重拟合、各分项排序能力与 L2 重加权 AUC。阈值优化以「同分段必须同档」为约束，分项缺失值按 50（中性）填充；标签为 sunsetbot 同口径自评，结果只代表与该标签的一致性，不代表独立真值验证。

## 许可证

本项目以 [MIT](LICENSE) 发布，© 2026 Felix2yu。
