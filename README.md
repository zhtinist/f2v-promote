# f2v-promote

微信视频号加热投放管理与自动提醒系统。实习期间参与开发的后台服务，覆盖从 AI 生成定向标签、批量创建投放计划，到订单状态轮询、自动投放检测与飞书提醒的完整链路。

## 功能概览

| 模块 | 说明 |
| --- | --- |
| **Web 管理台** | 活动创建、订单监控、作者/标签管理、投放策略配置 |
| **AI 标签生成** | 根据口播文案调用 OpenAI 生成多组定向标签 |
| **投放执行** | 对接诸葛智投 API，批量创建微信加热计划 |
| **订单轮询** | 定时检查支付与投放状态，超时自动告警 |
| **自动投放** | 监测视频播放增量，命中策略后自动创建投放或推送审核卡片 |
| **自动关停** | 按 ROI / 消耗规则自动停止低效计划 |
| **飞书通知** | Webhook 与交互卡片，支持一键审批投放 |
| **数据同步** | 定时从飞书表格同步视频统计数据 |

## 技术栈

- **语言 / 框架**：Go 1.25 · Gin · GORM
- **数据库**：MySQL 8.0
- **AI**：OpenAI API
- **消息队列**：阿里云 MNS
- **部署**：Docker Compose（本地）· 阿里云函数计算 FC3（生产）
- **通知**：飞书 Bot / 交互卡片

## 项目结构

```
f2v-promote/
├── cmd/
│   ├── server/              # Web API + 管理台
│   ├── cron/                # 定时任务（check-order / stats / promote / dispatcher / auto-stop）
│   ├── worker/              # 异步 Worker（create-order）
│   └── tag-classify/        # 标签分类工具
├── internal/                # 业务逻辑（handler / service / repository / model）
├── templates/               # 管理台 HTML 模板
├── static/                  # 前端静态资源
├── deploy/                  # 阿里云 FC 部署配置（Serverless Devs）
├── env/                     # 生产环境变量（prod.json 不入库，见 example）
├── plans/                   # 需求与设计文档
├── docs/                    # 架构规范等补充文档
├── docker-compose.yml       # 本地 MySQL
├── Taskfile.yml             # 构建与部署任务
└── .env.example             # 本地开发环境变量模板
```

## 快速开始

### 前置依赖

- Go 1.25+
- Docker（本地 MySQL）
- [Task](https://taskfile.dev/)（可选，用于快捷命令）

### 本地运行

```bash
# 1. 启动 MySQL
docker compose up -d

# 2. 配置环境变量
cp .env.example .env
# 编辑 .env，填入 OpenAI Key、诸葛账号等

# 3. 启动服务
go run ./cmd/server/
# 或使用 Task：task run

# 访问管理台（默认 :8000 或 .env 中 APP_PORT）
```

### 定时任务（本地调试）

```bash
go run ./cmd/cron/check-order/   # 订单状态轮询
go run ./cmd/cron/stats/         # 飞书数据同步
go run ./cmd/cron/promote/       # 自动投放检测
go run ./cmd/cron/dispatcher/    # 投放任务分发
go run ./cmd/cron/auto-stop/     # 自动关停
```

## 生产部署

部署配置位于 `deploy/`，基于 [Serverless Devs](https://www.serverless-devs.com/) 发布到阿里云函数计算。

```bash
# 准备生产环境变量（含真实密钥，勿提交 Git）
cp env/prod.example.json env/prod.json

# 部署 API
task deploy-api

# 部署单个 Cron（例：check-order）
task deploy-cron NAME=check-order

# 部署全部
task deploy-all
```

部署前请在 `deploy/*.prod.yaml` 中填写 VPC、日志项目等阿里云资源 ID。

## 文档

- [项目需求概览](plans/plan-project-overview.md)
- [自动投放方案](plans/plan-auto-promote.md)
- [飞书统计同步](plans/plan-stats-feishu-sync.md)
- [架构规范](docs/architecture-rules.md)

## 说明

本项目为实习期间参与的企业内部工具，部分第三方 API（诸葛智投、飞书等）需相应账号与权限方可完整运行。仓库中不包含任何生产密钥，请通过 `.env` 与 `env/prod.json` 自行配置。

## License

MIT
