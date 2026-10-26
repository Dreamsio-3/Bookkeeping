# 个人记账

一个轻量的个人收支记账应用：记录收入、支出、时间、收入人和支出人，支持按条件筛选统计，并导出 Excel / CSV。

- 单文件 Go 服务，前端页面直接编译进二进制，无需额外静态服务器
- SQLite 存储，数据落在 Docker 命名卷里，重启、重建容器都不丢
- 运行时内存实测约 **5–10 MB**
- 每日自动备份，默认保留 7 天
- 可选口令保护（HTTP Basic Auth）

## 快速开始

```bash
docker compose up -d --build
```

打开 <http://localhost:8080>。

数据保存在命名卷 `bookkeeping-data` 中：

```bash
# 查看数据文件
docker run --rm -v bookkeeping-data:/data alpine ls -lh /data

# 手动备份到当前目录
docker run --rm -v bookkeeping-data:/data -v "$PWD":/backup alpine \
  cp /data/bookkeeping.db /backup/bookkeeping-$(date +%F).db
```

停止服务但保留数据：`docker compose down`（不要加 `-v`，否则会删除数据卷）。

## 设置访问口令

编辑 `docker-compose.yml`，取消注释并修改：

```yaml
environment:
  APP_PASSWORD: "你的口令"
```

重新 `docker compose up -d` 后，浏览器会弹出登录框，用户名任意，密码为设置的口令。

## 环境变量

| 变量 | 默认值 | 说明 |
| --- | --- | --- |
| `PORT` | `8080` | 监听端口 |
| `DATA_DIR` | `/data` | 数据目录 |
| `DB_PATH` | `/data/bookkeeping.db` | 数据库文件路径 |
| `BACKUP_DIR` | `/data/backups` | 自动备份目录 |
| `BACKUP_KEEP_DAYS` | `7` | 备份保留份数，`0` 表示关闭自动备份 |
| `APP_PASSWORD` | 空 | 访问口令，留空则不校验 |
| `TZ` | `Asia/Shanghai` | 时区（镜像内已内置时区数据库） |

## 功能说明

- **记账**：类型（收入/支出）、金额、时间、收入人或支出人、分类、备注。金额按「分」存整数，不会出现浮点误差。
- **筛选**：日期范围、类型、人员、分类、关键词（备注/人员/分类），筛选项由数据库中的历史数据驱动，不写死。
- **统计**：当前筛选范围内的收入合计、支出合计、结余、笔数。
- **导出**：CSV 带 UTF-8 BOM，Excel 双击打开不乱码；Excel 为真正的 `.xlsx`，金额是数值单元格可直接求和，首行冻结并带筛选。
- **删除**：需要二次确认。

## 本地开发

需要 Go 1.24+：

```bash
go run .            # 默认监听 :8080，数据写入 ./data
go test ./...       # 运行全部测试
```

不想装 Go 也可以用容器执行：

```bash
docker run --rm -v "$PWD":/src -v bookkeeping-gomod:/go/pkg/mod -w /src \
  -e GOPROXY=https://goproxy.cn,direct golang:1.24-alpine go test ./...
```

## 接口

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| GET | `/api/entries` | 列表，支持 `from` `to` `kind` `person` `category` `keyword` `limit` `offset` |
| POST | `/api/entries` | 新增 |
| GET | `/api/entries/{id}` | 详情 |
| PUT | `/api/entries/{id}` | 修改 |
| DELETE | `/api/entries/{id}` | 删除 |
| GET | `/api/summary` | 合计，参数同列表 |
| GET | `/api/meta` | 人员、分类候选值 |
| GET | `/api/export?format=csv\|xlsx` | 导出，参数同列表 |
| GET | `/api/health` | 健康检查（不校验口令） |

请求示例：

```bash
curl -X POST http://localhost:8080/api/entries \
  -H "Content-Type: application/json" \
  -d '{"kind":"expense","amountCents":2580,"occurredAt":"2026-10-26T12:00","person":"张三","category":"餐饮","note":"午饭"}'
```

所有 ID 均以字符串返回，避免大整数在前端丢失精度。

## 数据与恢复

- 数据库：`/data/bookkeeping.db`（WAL 模式，退出时自动合并回主库）
- 自动备份：`/data/backups/bookkeeping-YYYY-MM-DD.db`，每天 03:00 生成一份，用 `VACUUM INTO` 生成一致性快照，不影响使用
- 恢复：停止容器，把备份文件复制为 `/data/bookkeeping.db`（同时删除 `-wal`、`-shm` 文件），再启动容器

```bash
docker compose down
docker run --rm -v bookkeeping-data:/data -v "$PWD":/backup alpine sh -c \
  "rm -f /data/bookkeeping.db-wal /data/bookkeeping.db-shm && cp /backup/bookkeeping-2026-10-26.db /data/bookkeeping.db"
docker compose up -d
```

## 目录结构

```
main.go                     启动、优雅关闭、嵌入前端
internal/db/                数据库连接与迁移（PRAGMA user_version）
internal/ledger/            业务逻辑：校验、筛选、统计
internal/httpapi/           HTTP 接口与鉴权
internal/export/            CSV 与 xlsx 生成
internal/backup/            每日备份与清理
web/                        HTML / CSS / JS（无构建步骤）
```
