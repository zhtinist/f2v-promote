# f2v-promote: 视频号加热自动投放系统（Go 版）

## 概述

多用户 Web 应用，用户提供视频口播文案 → OpenAI 生成多组定向标签 → 调用诸葛智投 API 批量创建投放计划 → 自动轮询支付/投放状态。

## 技术栈

| 层 | 选型 |
|---|---|
| 后端 | Go 1.25 + Gin |
| ORM | GORM + MySQL 8.0 |
| AI | OpenAI API (gpt-4o) |
| 投放 | 诸葛智投 REST API（浏览器 Header 伪装） |
| 定时任务 | 阿里云 FC（每分钟触发）/ 本地 goroutine |
| 认证 | Session Cookie（securecookie） |
| 通知 | 飞书/钉钉/企微 Webhook |
| 部署 | Docker Compose (MySQL) + 二进制直接运行 |

## 项目结构

```
f2v-promote/
├── cmd/
│   ├── server/main.go              # API 服务入口（:8000）
│   └── cron/main.go                # 定时轮询入口（阿里云 FC 兼容）
├── internal/
│   ├── config/config.go            # .env 配置加载
│   ├── model/
│   │   ├── user.go
│   │   ├── campaign.go             # 活动（含 account_id）
│   │   ├── order.go                # 订单状态机（含 account_id, latest_detail, latest_record）
│   │   ├── zhuge_account.go        # 诸葛账号（多账号，含 token）
│   │   ├── zhuge_author.go         # 作者缓存（按账号）
│   │   ├── zhuge_tag.go            # 标签缓存
│   │   ├── audit_log.go
│   │   ├── performance_log.go
│   │   └── prompt_template.go
│   ├── repository/
│   │   ├── campaign_repo.go
│   │   ├── order_repo.go           # 含 GetNeedPolling / GetStatsByUserID / GetRecentByUserID
│   │   ├── user_repo.go
│   │   ├── zhuge_account_repo.go   # 账号 CRUD + UpdateToken
│   │   ├── zhuge_author_repo.go    # 作者 ListByAccountID + ReplaceByAccountID
│   │   ├── zhuge_tag_repo.go
│   │   ├── audit_repo.go
│   │   └── prompt_repo.go
│   ├── service/
│   │   ├── dto.go                  # 请求/响应类型（TagGroup, CreatePlanRequest, *Result）
│   │   ├── campaign.go             # 活动编排（CreateWithAITags, Confirm, SubmitOrder, CheckPayment, GetDetail, GetRecord）
│   │   ├── zhuge_client.go         # 诸葛 API 客户端（多账号 token，按 accountID 调用）
│   │   ├── cron.go                 # CronService：15s 轮询，完整日志，record 轮询
│   │   ├── openai.go               # AI 标签生成（返回 []TagGroup）
│   │   ├── plan_builder.go         # BuildPlanData（返回 *CreatePlanRequest）+ RateLimiter
│   │   ├── notifier.go             # Webhook 通知
│   │   └── auth.go                 # Session token 管理
│   ├── handler/
│   │   ├── router.go               # 路由注册
│   │   └── v1/
│   │       ├── zhuge.go            # 活动创建/确认、作者/视频查询
│   │       ├── order.go            # 订单提交/查支付/更新状态/详情/记录/列表/导出
│   │       ├── campaign.go         # 活动列表/导出
│   │       ├── account.go          # 诸葛账号 CRUD + 刷新作者
│   │       ├── dashboard.go        # Dashboard 统计 + 最新订单
│   │       ├── auth.go             # 登录/注册/登出
│   │       ├── page.go             # HTML 页面渲染
│   │       └── prompt.go           # Prompt 模板 CRUD
│   ├── middleware/auth.go          # Session Cookie 鉴权中间件
│   └── pkg/render.go               # HTML 模板加载
├── templates/
│   ├── base.html                   # 公共布局（5 tab 侧边栏）
│   ├── dashboard.html              # 统计面板
│   ├── create.html                 # 创建投放（4 步流程）
│   ├── campaigns.html              # 活动管理
│   ├── orders.html                 # 订单监控
│   ├── settings.html               # 系统设置（待改造：三 Tab）
│   └── login.html                  # 登录页
├── static/css/style.css
├── mysql/init.sql                  # DDL + 迁移脚本
├── docker-compose.yml
├── Taskfile.yml
└── .air.toml                       # 热重载配置
```

## 诸葛平台 plan_status 映射（来源：chunk-dd2a6c58）

| plan_status | 诸葛含义 | 我方处理 → DB status |
|---|---|---|
| 0 | 已结束 | pending/init → checkPayment; 其他 → `closed` |
| 1 | 加热中 | → `active` |
| 2 | 离线 | → `closed` |
| 3 | 待支付 | → `pending`（保持） |
| 5 | 审核中 | → `review` |
| 6 | 审核不通过 | → `failed` |
| 7 | 已删除 | → `closed` |
| 8 | 已完成 | → `closed` |
| 9 | 退款中 | → `closed` |
| 10 | 待加热 | → `review`（等待上线） |

**auto_close_exec_status：**

| auto_close | 含义 | 处理 |
|---|---|---|
| 0 | 未使用 | 不处理 |
| 1 | 标准 | 不处理 |
| 2 | 失败 | → `closed` |
| 3 | 已完成 | → `closed` |
| 4 | 进行中 | 不处理 |

**pay_status（poll_plan_pay_status 接口）：**

| pay_status | 含义 | 处理 |
|---|---|---|
| 0 | 未支付 | 保持 `pending` |
| 1 | 已支付 | → `review` |
| 3,4,5 | 支付失败 | → `failed` |

## 订单状态机

```
init ──(submit)──→ pending ──(pay_status=1)──→ review ──(plan_status=1)──→ active ──(plan结束)──→ closed
  │                   │                          ↑                            │
  │                   │                    plan_status=5/10                   │
  │                   └──(pay_status=3,4,5)──→ failed ←──(plan_status=6)─────┘
  │
  └──(create_plan 失败)──→ failed
```

| 状态 | 含义 | 触发条件 |
|---|---|---|
| `init` | 已创建 DB 记录，未调诸葛 | confirm 时创建 |
| `pending` | 已调 CreatePlan，等待支付 | submit 成功 / plan_status=3 |
| `review` | 已支付，等待平台审核上线 | pay_status=1 / plan_status=5,10 |
| `active` | 加热中 | plan_status=1 |
| `closed` | 计划结束/关闭 | plan_status=0,2,7,8,9 / auto_close=2,3 |
| `failed` | 创建或支付失败（终态） | CreatePlan 异常 / pay_status=3,4,5 / plan_status=6 |

## 页面设计

### Dashboard 统计面板（`/` → dashboard.html）

首页，展示全局概览 + 最新订单动态。

**统计卡片（顶部一排）：**

| 指标 | 数据来源 | 说明 |
|---|---|---|
| 活动总数 | `COUNT(campaigns)` | 全部活动 |
| 投放中 | `COUNT(orders WHERE status='active')` | 加热中的订单 |
| 待支付 | `COUNT(orders WHERE status='pending')` | 等待用户去支付 |
| 审核中 | `COUNT(orders WHERE status='review')` | 已支付等待上线 |
| 失败/异常 | `COUNT(orders WHERE status='failed')` | 创建或支付失败 |
| 总消耗 | `SUM(latest_detail.cost)` | 所有 active/closed 订单的消耗 |

**最新 50 个订单列表（下方表格）：**

| 列 | 字段 | 说明 |
|---|---|---|
| 标签组 | `tag_group.name` | 标签名 |
| 状态 | `status` | 带颜色标签 |
| 消耗 | `latest_detail.cost` | 从 DB 读 |
| ROI | `latest_detail.roi` | |
| 观看 | `latest_detail.view_num` | |
| 关注 | `latest_detail.focus_num` | |
| 创建时间 | `created_at` | |
| 操作 | 查看详情 | 跳转或抽屉 |

**后端 API：**
```
GET /api/dashboard/stats
  ← {campaigns, active, pending, review, failed, total_cost}

GET /api/dashboard/recent-orders?limit=50
  ← {orders: [{id, tag_name, status, cost, roi, view_num, focus_num, created_at}, ...]}
```

> 两个聚合接口，一次 SQL 查完，不走 N+1。30s 自动刷新。

### 创建投放页面（`/create` → create.html）

独立页面，完整的创建流程。从 dashboard 拆出来，dashboard 只做展示。

**步骤流程：**
```
Step 1: 输入文案 + 选账号
  → 填写口播文案、选择诸葛账号、设置标签组数量
  → 点击"生成标签" → POST /zhuge/create

Step 2: 预览标签组
  → 展示 AI 生成的 N 组标签，可删除不需要的
  → 点击"下一步"

Step 3: 选择作者和视频
  → 从已选账号的作者列表中选作者（读 DB）
  → 加载该作者的视频列表（调第三方）
  → 选择视频

Step 4: 确认并提交
  → 显示汇总：账号、文案摘要、标签组数、作者、视频
  → 点击"确认投放" → POST /zhuge/:id/confirm → 创建 N 个 init 订单
  → 自动串行 submit（每笔间隔 10s）
  → 进度条展示：N 个待支付 / M 个失败
  → 完成后跳转到订单列表页
```

## 核心流程

### 1. 创建活动 → AI 生成标签

```
POST /zhuge/create  (account_id, script, cost_threshold, tag_group_count)
  → ZhugeClient.GetFlatTags()         # 三级缓存：内存→DB→API
  → OpenAIService.GenerateZhugeTags() # GPT 从可用标签中选出 N 组
  → CampaignRepo.Create()             # 状态 pending
  ← {campaign_id, tag_groups, tag_map}
```

### 2. 确认投放 → 批量建订单

```
POST /zhuge/:campaign_id/confirm
  → 解析 campaign.TagGroups
  → for each tag_group: OrderRepo.CreateZhuge()  # N 条 init 订单
  → CampaignRepo.UpdateStatus("running")
  ← {campaign_id, orders: [{id, status: "init"}, ...]}
```

### 3. 前端批量提交订单

前端串行逐个 submit，**每笔间隔 10 秒**，submit 成功（status=pending）即创建下一笔，**不等待支付**。
支付和后续状态流转全部由 cron 后台处理。

```
前端 processOrders:
  for each order (status=init):
    if 非首笔 → 等待 10 秒（倒计时显示）
    POST /zhuge/order/:order_id/submit (video_json, author_json)
      → 后端 RateLimiter.WaitIfNeeded()
      → BuildPlanData → ZhugeClient.CreatePlan() → batch_id
      → OrderRepo.UpdateZhugeSubmitted()  # init → pending
      ← {order_id, status: "pending", batch_id}

    status=pending → continue 下一个
    status=failed  → 跳过，continue 下一个

  全部提交完成 → 显示汇总（N 个待支付 / M 个失败）
```

> **前端不做支付轮询**。用户提交完所有订单后，去诸葛平台统一支付。
> 支付状态由 cron 15s 一轮自动检测并更新 DB。
> 前端订单列表页（orders.html）30s 自动刷新展示最新状态。

### 4. Cron 后台状态轮询

```
CronService.Run()  58 秒执行窗口，每 15 秒一轮：
  1. OrderRepo.GetNeedPolling(20, 15s)
     WHERE status NOT IN (init, failed, closed)
       AND (platform_order_id OR batch_id 不为空)
       AND query_at 距今 > 15s

  2. for each order:
     a. 无 platform_order_id → tryFetchPlatformOrder()
        → GetBatchOrders(batch_id) → 提取 platform_order_id + pay_url

     b. GetPlanDetail(platform_order_id)
        按 plan_status 映射：
        - 1(加热中) → active
        - 5,10(审核中/待加热) → review
        - 3(待支付) + pending → checkPayment()
        - 0(已结束) + pending → checkPayment()
        - 6(审核不通过) → failed
        - 0,2,7,8,9(结束/离线/删除/完成/退款) → closed
        - auto_close=2,3 → closed

     c. active 订单额外查 GetPlanRecord → latest_record

     d. TouchQueryAt()
```

### 5. 前端状态展示（纯读 DB）

```
GET /zhuge/order/:order_id/check-payment  → 读 DB 状态
GET /zhuge/orders                         → 全部订单列表
```

> 前端 orders.html 每 30s 自动刷新订单列表，展示 cron 写入的最新状态。
> check-payment **不调第三方 API**，只返回 DB 中的最新状态。
> 前端可高频轮询此接口而不会触发限流问题。

### 6. 查看投放数据（读 DB 缓存）

```
GET /zhuge/order/:order_id/detail
  → 读 order.latest_detail 字段（cron 覆盖写，不调第三方）
  ← {order_id, detail: {cost, ROI, click_num, ...}, query_at, msg?}

GET /zhuge/order/:order_id/record
  → 读 order.latest_record 字段（cron 覆盖写，不调第三方）
  ← {order_id, record: [{create_time, content, typeLabel, ...}], query_at, msg?}
```

> cron 负责定期写入 latest_detail / latest_record，handler 直接读这两个字段。

## 路由总览

### 公开

| 方法 | 路径 | Handler | 说明 |
|---|---|---|---|
| GET | `/login` | Auth.LoginPage | 登录页 |
| POST | `/login` | Auth.Login | 登录 |
| POST | `/register` | Auth.Register | 注册 |
| GET | `/logout` | Auth.Logout | 登出 |

### 页面（需认证）

| 方法 | 路径 | 模板 | 说明 |
|---|---|---|---|
| GET | `/` | dashboard.html | 统计面板：汇总数据 + 最新 50 个订单 |
| GET | `/create` | create.html | 创建投放：文案→标签→选账号/作者/视频→提交 |
| GET | `/orders-page` | orders.html | 全部订单列表 |
| GET | `/campaigns-page` | campaigns.html | 活动管理 |
| GET | `/settings-page` | settings.html | 系统设置（账号管理、Prompt 模板） |

### 诸葛 API（需认证）

| 方法 | 路径 | Handler | 说明 |
|---|---|---|---|
| GET | `/zhuge` | Zhuge.ListCampaigns | 活动列表 |
| GET | `/zhuge/authors` | Zhuge.GetAuthors | 绑定作者列表 |
| POST | `/zhuge/videos` | Zhuge.GetVideos | 作者视频列表 |
| POST | `/zhuge/create` | Zhuge.Create | 创建活动 + AI 标签 |
| POST | `/zhuge/:id/confirm` | Zhuge.Confirm | 确认投放 → 建订单 |
| GET | `/zhuge/orders` | Order.ListOrders | 全部订单 |
| GET | `/zhuge/orders/export` | Order.ExportCSV | 导出 CSV |
| POST | `/zhuge/order/:id/submit` | Order.Submit | 提交到诸葛 |
| GET | `/zhuge/order/:id/check-payment` | Order.CheckPayment | 查支付状态 |
| POST | `/zhuge/order/:id/update-status` | Order.UpdateStatus | 手动改状态 |
| GET | `/zhuge/order/:id/detail` | Order.GetDetail | 投放数据 |
| GET | `/zhuge/order/:id/record` | Order.GetRecord | 操作日志 |

### Dashboard API（需认证）

| 方法 | 路径 | Handler | 说明 |
|---|---|---|---|
| GET | `/api/dashboard/stats` | Dashboard.Stats | 统计汇总（活动数、各状态订单数、总消耗） |
| GET | `/api/dashboard/recent-orders?limit=50` | Dashboard.RecentOrders | 最新 N 个订单（含 cost/roi 等） |

### 诸葛账号 API（需认证）

| 方法 | 路径 | Handler | 说明 |
|---|---|---|---|
| GET | `/zhuge/accounts` | Account.List | 账号列表 |
| POST | `/zhuge/accounts` | Account.Create | 添加账号 |
| PUT | `/zhuge/accounts/:id` | Account.Update | 修改账号 |
| DELETE | `/zhuge/accounts/:id` | Account.Delete | 删除账号 |
| POST | `/zhuge/accounts/:id/refresh-authors` | Account.RefreshAuthors | 刷新作者 |
| GET | `/zhuge/accounts/:id/authors` | Account.ListAuthors | 作者列表（读 DB） |

### 其他 API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET/POST/DELETE | `/campaigns/**` | 活动管理 |
| GET/POST/DELETE | `/prompts/**` | Prompt 模板 CRUD |
| GET/POST/DELETE | `/video-stats/**` | 视频统计导入/查询 |

## 诸葛账号与作者管理

### 当前实现：多账号 CRUD + 作者缓存

```
zhuge_accounts 表                     zhuge_authors 表
┌──────────────────────┐             ┌──────────────────────────┐
│ id (UUID PK)         │──1:N──→    │ id (VARCHAR PK, 诸葛ID)    │
│ name (显示名)         │             │ account_id (FK)           │
│ account (手机号)      │             │ nickname                  │
│ password (加密)       │             │ avatar_url                │
│ ts_user_id           │             │ platform (weixin)         │
│ group_id             │             │ raw_data (JSON, 完整响应)   │
│ token (当前JWT)       │             │ cached_at                 │
│ token_expires_at     │             │ created_at                │
│ status (active/inactive)│          └──────────────────────────┘
│ created_at           │
│ updated_at           │
└──────────────────────┘
```

**流程：**

1. **添加账号**：用户填写 account + password + ts_user_id → 存 DB → 立即调 login 验证 → 成功则 token 写入
2. **刷新作者列表**：选账号 → 用该账号 token 调 `GetHistoryAuthors` → 解析响应 → 覆盖写 `zhuge_authors` 表
3. **创建活动时**：选账号 + 选作者（从 DB 读，不调第三方）→ 用该账号的 token + ts_user_id 创建计划
4. **Token 刷新**：cron 检测 `token_expires_at` 即将过期的账号 → 自动 re-login

**ZhugeClient**：按账号动态获取 token，所有 API 方法接收 `accountID`。
`EnsureTokenForAccount(account)` → 查 DB token → 过期则 re-login → 更新 DB。

### 路由

| 方法 | 路径 | Handler | 说明 |
|---|---|---|---|
| GET | `/zhuge/accounts` | Account.List | 账号列表 |
| POST | `/zhuge/accounts` | Account.Create | 添加账号（验证 login） |
| PUT | `/zhuge/accounts/:id` | Account.Update | 修改账号 |
| DELETE | `/zhuge/accounts/:id` | Account.Delete | 删除账号 |
| POST | `/zhuge/accounts/:id/refresh-authors` | Account.RefreshAuthors | 刷新作者列表 |
| GET | `/zhuge/accounts/:id/authors` | Account.ListAuthors | 该账号下的作者列表（读 DB） |

### 前端（settings.html 三个 Tab）

settings.html 顶部 Tab 栏切换三个区块：**账号管理** / **作者管理** / **提示词模板**

#### Tab 1: 账号管理

**列表表格：**

| 列 | 字段 | 说明 |
|---|---|---|
| 名称 | `name` | 显示名 |
| 账号 | `account` | 手机号 |
| TS User ID | `ts_user_id` | 诸葛手投账号 ID |
| Group ID | `group_id` | 分组 ID |
| 状态 | `status` | active/inactive，带颜色标签 |
| Token 过期 | `token_expires_at` | 距过期时间，过期显示红色 |
| 操作 | 编辑 / 删除 | |

**操作：**
- 右上角"+ 添加账号"按钮 → 打开抽屉
- 抽屉表单：名称、账号（手机号）、密码、ts_user_id、group_id
- 提交时后端验证 login → 成功才保存
- 编辑：同抽屉，回填已有数据
- 删除：二次确认

**API 调用：**
```
GET    /zhuge/accounts           → 列表
POST   /zhuge/accounts           → 添加（后端自动验证 login）
PUT    /zhuge/accounts/:id       → 修改
DELETE /zhuge/accounts/:id       → 删除
```

#### Tab 2: 作者管理

**顶部：** 账号下拉选择器（选择要查看哪个账号的作者）+ "刷新作者列表"按钮

**列表表格：**

| 列 | 字段 | 说明 |
|---|---|---|
| 头像 | `avatar_url` | 圆形小图 |
| 昵称 | `nickname` | |
| Username | `username` | 诸葛内部标识 |
| 平台 | `platform` | weixin |
| 缓存时间 | `cached_at` | 上次刷新时间 |

**操作：**
- 选择账号 → `GET /zhuge/accounts/:id/authors` → 展示缓存的作者列表
- 点击"刷新作者列表" → `POST /zhuge/accounts/:id/refresh-authors` → 从诸葛 API 拉取最新 → 覆盖 DB → 刷新表格
- 刷新按钮带 loading 状态

**API 调用：**
```
GET  /zhuge/accounts/:id/authors          → 读 DB 缓存
POST /zhuge/accounts/:id/refresh-authors  → 调第三方拉取 → 覆盖 DB → 返回新列表
```

#### Tab 3: 提示词模板（已实现）

现有的 Prompt 模板 CRUD，保持不变。

## 诸葛 API 客户端

**ZhugeClient** (`internal/service/zhuge_client.go`)

- Token 管理：按账号从 DB 读取，过期自动 re-login
- 浏览器 Header 伪装（Chrome UA + Sec-Fetch-* 全套）
- 两个 Base URL：`ZHUGE_LOGIN_URL`（登录/详情）+ `ZHUGE_API_BASE`（业务接口）

| 方法 | 接口 | 说明 |
|---|---|---|
| `GetHistoryAuthors(accountID)` | POST /api/v1/wechat/history-author | 绑定作者 |
| `GetAuthorVideos(accountID, username)` | POST /api/v1/wechat/author-videos | 作者视频 |
| `GetInterestTags(accountID)` | POST /api/v3/wechat/interest-tags | 兴趣标签树 |
| `GetFlatTags(accountID)` | (缓存层) | 标签名→ID 映射 |
| `CreatePlan(accountID, req)` | POST /api/v5/wechat/create-plan | 创建投放计划 → batch_id |
| `GetBatchOrders(accountID, batchID)` | POST /api/v5/wechat/batch-data-list/:id | 批次详情 |
| `GetPlanDetail(accountID, promotionID)` | GET /api/v1/video-plan/detail/:id | 计划详情 |
| `GetPlanRecord(accountID, promotionID)` | GET /api/v1/video-plan/plan_record/:id | 操作日志 |
| `PollPaymentStatus(accountID, promotionID)` | GET /api/v5/wechat/poll_plan_pay_status/:id | 支付状态 |

## 限流策略

`RateLimiter`（`internal/service/plan_builder.go`）

- 滑动窗口：5 分钟内最多 N 条（配置 `ZHUGE_MAX_PLANS_PER_5MIN`，默认 5）
- 最小间隔：10 秒（配置 `ZHUGE_PLAN_INTERVAL`）
- 仅 SubmitOrder 调用 CreatePlan 前等待

## 数据库核心表

```sql
-- 用户
users (id, username, hashed_password, cost_threshold, created_at)

-- 诸葛账号（替代 .env 单例 + zhuge_tokens 表）
zhuge_accounts (id UUID PK, name, account, password, ts_user_id, group_id,
                token, token_expires_at, status VARCHAR, created_at, updated_at)

-- 诸葛作者缓存（按账号）
zhuge_authors (id VARCHAR PK, account_id FK, nickname, avatar_url, platform,
              raw_data JSON, cached_at, created_at)

-- 活动（一次文案提交 = 一个活动）
campaigns (id, user_id, account_id FK, script, tag_groups JSON, status, error_msg, created_at, finished_at)

-- 订单（一个标签组 = 一个订单）
orders (id, campaign_id, user_id, account_id FK, tag_group JSON,
        platform_order_id, batch_id, pay_url, status, close_reason,
        source, create_response JSON, query_response JSON,
        latest_detail JSON, latest_record JSON, query_at, created_at)

-- 标签缓存
zhuge_tags (id, text, parent_id, parent_text, level)

-- 审计日志
audit_logs (id, user_id, action, detail JSON, created_at)
```

> `zhuge_tokens` 表废弃，token 直接存在 `zhuge_accounts` 表中。
> `campaigns` 和 `orders` 新增 `account_id` 关联到诸葛账号，cron 轮询时使用对应账号的 token。

## 构建与运行

```bash
# MySQL
task mysql          # docker-compose up
task migrate        # 加载 init.sql

# 开发
task run            # 构建并启动 API 服务 (:8000)
task run-cron       # 构建并启动本地轮询
task run-all        # 两者并行
task dev            # air 热重载

# 构建
task build          # → bin/f2v-promote
task build-cron     # → bin/f2v-promote-cron
```

## 依赖

```
gin v1.10.0          # HTTP 框架
gorm v1.31.1         # ORM
gorm/mysql v1.6.0    # MySQL 驱动
go-openai v1.41.2    # OpenAI SDK
uuid v1.6.0          # UUID
securecookie v1.1.2  # Session 加密
godotenv v1.5.1      # .env 加载
datatypes v1.2.7     # GORM JSON 类型
```

## 架构原则：API 服务 vs Cron 的职责划分

```
┌─────────────────────────────────────────────────────────┐
│                    API 服务 (cmd/server)                  │
│                                                          │
│  用户主动操作（必须调第三方）：                              │
│    POST /zhuge/create       → OpenAI + ZhugeClient        │
│    POST /zhuge/:id/confirm  → 纯 DB（不调第三方）          │
│    POST /order/:id/submit   → ZhugeClient.CreatePlan      │
│    GET  /zhuge/authors      → ZhugeClient.GetHistoryAuthors│
│    POST /zhuge/videos       → ZhugeClient.GetAuthorVideos  │
│                                                          │
│  状态查询（只读 DB，不调第三方）：                           │
│    GET /order/:id/check-payment  → 读 DB 状态              │
│    GET /order/:id/detail         → 读 DB 缓存的 detail     │
│    GET /order/:id/record         → 读 DB 缓存的 record     │
│    GET /orders                   → 读 DB                  │
│    POST /order/:id/update-status → 写 DB                  │
└─────────────────────────────────────────────────────────┘

┌─────────────────────────────────────────────────────────┐
│                    Cron 服务 (cmd/cron)                   │
│                                                          │
│  所有第三方轮询集中在这里：                                 │
│    1. GetBatchOrders   → 获取 platform_order_id          │
│    2. GetPlanDetail    → 获取计划状态/消耗数据 → 写入 DB    │
│    3. PollPaymentStatus → 获取支付状态 → 写入 DB           │
│    4. GetPlanRecord    → 获取操作日志 → 写入 DB             │
│                                                          │
│  轮询结果统一写入 orders 表：                               │
│    - status 字段：状态流转                                 │
│    - query_response JSON：追加每次轮询的完整响应             │
│    - query_at：最后查询时间                                │
└─────────────────────────────────────────────────────────┘
```

**核心规则**：前端轮询 API 服务获取状态 → API 服务只读 DB → Cron 负责写入最新状态。
前端可以高频轮询 check-payment / detail 而不触发第三方限流。

## 架构设计备忘

> 以下为已完成改造的设计决策记录，不再作为待办。

**API 不调第三方轮询**：check-payment / detail / record 纯读 DB，状态流转全部由 cron 写入。

**类型体系**：service 层使用 `dto.go` 类型化 struct（`TagGroup`、`*SubmitOrderResult` 等），
video/author 用 `json.RawMessage` 透传。

**多账号**：`ZhugeClient` 所有方法接收 `accountID`，从 `zhuge_accounts` 表读 token，过期自动 re-login。

**Cron 日志**：每笔订单 poll 入口/出口/第三方响应关键字段/状态变化/no_change 全部打日志，格式 `[cron] <动作> order=<短ID> ...`。

**Cron 频率**：`minQueryGap=15s`，同一订单 15s 内不重复查。active 订单同时查 detail + record。

**前端提交**：串行 submit 每笔间隔 10s，不等支付。支付状态由 cron 后台写入，前端订单列表 30s 刷新。

## 已完成

- [x] 项目骨架：cmd/server + cmd/cron 双入口
- [x] 用户认证：注册/登录/Session Cookie
- [x] 活动创建：文案 → OpenAI → 标签组 → Campaign
- [x] 活动确认：选视频/作者 → 批量创建 init 订单
- [x] 订单提交：逐个调诸葛 CreatePlan → pending（前端间隔 10s，不等支付）
- [x] 支付查询：cron 15s 轮询 + handler 纯读 DB
- [x] 状态流转：全部 10 个 plan_status + auto_close 映射
- [x] 诸葛客户端：多账号 token 管理、401 重试、浏览器伪装
- [x] 标签缓存：三级缓存（内存→DB→API）
- [x] 限流器：滑动窗口
- [x] 订单列表/CSV 导出
- [x] Prompt 模板 CRUD
- [x] 审计日志
- [x] CheckPayment 纯读 DB
- [x] 消除 `map[string]interface{}`：dto.go + 全部 service/handler 签名类型化
- [x] GetDetail / GetRecord 改为读 DB 缓存（latest_detail / latest_record）
- [x] Cron 日志增强：每笔订单入口/出口/第三方响应/状态变化/no_change 完整日志
- [x] Cron record 轮询 + latest_detail/latest_record 覆盖写
- [x] 诸葛账号 CRUD（zhuge_accounts 表 + handler/account.go 6 个路由）
- [x] 作者缓存（zhuge_authors 表 + 刷新作者 API）
- [x] ZhugeClient 多账号改造：所有方法加 accountID，去掉全局单例 token
- [x] campaigns + orders 表加 account_id
- [x] 页面拆分：dashboard.html（统计面板）+ create.html（创建投放独立页）
- [x] Dashboard API：/api/dashboard/stats + /api/dashboard/recent-orders
- [x] 侧边栏 5 tab：统计面板 / 创建投放 / 活动管理 / 订单监控 / 系统设置
- [x] 前端修复：错误字段、N+1→聚合 API、终态跳过轮询、Alpine.js 锁版本
- [x] settings.html 三 Tab 页面：账号管理 CRUD + 作者管理（下拉选账号+刷新）+ 提示词模板

## TODO

### 待实现

（无）

### 后续优化

- [ ] Context 透传：所有 service 方法加 `ctx context.Context`
- [ ] Sentinel Errors：替代 handler 层字符串匹配错误
- [ ] video-stats 路由加鉴权
- [ ] 飞书/钉钉通知集成（NotifierService 已就绪，未接入流程）
- [ ] 成本阈值自动关停（performance_log 表已建，逻辑未接）
- [ ] 支付超时自动标记 expired
