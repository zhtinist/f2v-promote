# 项目架构规范 (Project Architecture Rules)

本规则涵盖了本项目的基础架构指导、职责划分与长期演进的原则约定。

## 1. 模块树与职责划分

```text
project-root/
├── cmd/                        # 启动入口
│   ├── api/main.go             # API 服务入口
│   ├── dashboard/main.go       # Dashboard 面板服务入口
│   ├── cron/main.go            # 定时任务入口：go run cmd/cron/main.go -name=xxx
│   ├── migration/main.go       # 数据迁移部署入口
│   └── worker/main.go          # 异步 Worker 入口：go run cmd/worker/main.go -name=xxx
├── config/                     # 配置管理（共享）
│   ├── config.go               # 配置结构定义
│   └── setup.go                # 配置初始化和数据库连接
├── internal/
│   ├── types/                  # 共享 DTO（Common、Pagination 等）
│   ├── initializers/           # 依赖初始化 & 区域策略装配
│   ├── center/                 # 流程编排层（项目内部 pkg）
│   ├── cron/                   # 定时任务（registry + 各任务实现）
│   ├── worker/                 # 异步 Worker（registry + 各任务实现）
│   ├── migrations/             # 数据迁移/同步记录
│   ├── api/                    # === API 服务域 ===
│   │   ├── middleware/         #   中间件（auth / language / logger）
│   │   ├── routers/            #   路由层（薄层）
│   │   ├── service/            #   业务逻辑层
│   │   └── types/              #   API 私有 Req/Resp DTO
│   ├── dashboard/              # === Dashboard 服务域 ===
│   │   ├── middleware/         #   Dashboard 中间件
│   │   ├── handler/            #   路由处理
│   │   ├── service/            #   业务逻辑层
│   │   ├── types/              #   Dashboard 私有 DTO
│   │   └── templates/          #   Go 模版渲染
│   ├── models/                 # 数据模型 DO（共享，按表一个文件）
│   ├── repo/                   # 数据访问层（共享，接口驱动）
│   └── pkg/                    # 内部扩展包（errcode / i18n / helpers）
├── pkg/                        # 跨项目级公共库（firebase / logger 等）
├── test/                       # 测试文件
├── plans/                      # 需求 Plan 文档（YYYY-MM-DD-需求名称.md）
├── go.mod / go.sum
├── Dockerfile / docker-compose.yml
└── dev_env
```

**层级职责约束：**

- **Router/Handler（路由层）**: 仅参数绑定与响应渲染，不含业务逻辑。
- **Service（业务层）**: 核心，所有业务计算、规则检验、数据组装。
- **Repo（数据层）**: CRUD 操作，禁止业务逻辑。
- **Types/DTO（数据传输层）**: 统一使用 Req/Resp 结构体。
- **Center（编排层）**: 复杂流程编排，被各 Service 层调用。
- **Cron/Worker**: 统一入口 + 注册表分发，业务委托 center / service。

## 2. 模型共享策略

| 模型层 | 位置 | 职责 |
| --- | --- | --- |
| **DO** | `internal/models/` | 数据库表结构（ORM 映射标记） |
| **共享 DTO** | `internal/types/` | 跨域公共结构（Common、Pagination） |
| **API DTO** | `internal/api/types/` | API 的 Req/Resp |
| **Dashboard DTO** | `internal/dashboard/types/` | Dashboard 的 Req/Resp |

- DO 与 DTO 严格解耦，**DO 绝不暴露给 Handler/前端**
- DO ↔ DTO 转换由 **Service 层**负责
- DTO 必须带 `json` + `validate` tag
- 枚举与 DO 关联的放 `models/`，跨域常量放 `internal/pkg/constants/`

## 3. 接口抽象原则

| 层级 | 是否强制接口 | 说明 |
| --- | --- | --- |
| **Repo 层** | ✅ 强制 | 先定义 `IXxxRepo` 再实现 |
| **外部服务 pkg** | ✅ 强制 | 第三方服务封装为接口 |
| **Service 层** | ❌ | 直接结构体实现 |
| **Handler 层** | ❌ | 薄层无需抽象 |

- 接口以 `I` 开头（`IUserRepo`、`IUploadService`）
- 构造器返回接口类型：`func NewUserRepo(db DBConn) IUserRepo`
- 依赖单向流动：`Handler → Service → IRepo`
- 单个接口方法数不超过 8 个

## 4. 错误处理规范

| 层级 | 错误处理 | 日志 |
| --- | --- | --- |
| **Repo** | 原始错误；`ErrRecordNotFound` → `ErrNotFound` | ❌ |
| **Service** | `fmt.Errorf("get user: %w", ErrNotFound)` 包装 | ⚠️ 关键流程 Warn |
| **Handler** | `errcode.GetError(err).WithContext(ctx).Send(w)` | ✅ 兜底 |

- **Sentinel Errors**：`internal/pkg/errors/`
- **errcode**：`internal/pkg/errcode/`
- **链式 API**：`GetError(err).WithContext(ctx).WithT(ctx).Send(w)`
  - `GetError` 断言匹配，失败兜底 `ErrServer`
  - `WithContext` 挂上下文 + 打日志
  - `WithT` 可选，启用 i18n
  - `Send` 写 HTTP 响应

**错误码分段**：`0` 成功 / `1-1000` 通用业务错误（展示 msg）/ `> 1000` 特定分支（前端逻辑分叉）

**错误包装规范（`%w` vs `%v`）**：

| 层级 | 使用 `%w` | 使用 `%v` | 说明 |
|------|-----------|-----------|------|
| **Service 层** | ✅ 强制 | ❌ 禁止 | Handler 依赖 `errcode.GetError(err)` 解包匹配业务码，`%v` 截断错误链 → 兜底 500 |
| **Repo 层** | ✅ 强制 | ❌ 禁止 | Service 需要 `errors.Is(err, gorm.ErrRecordNotFound)` 判断 |
| **pkg/ 基础设施** | 可选 | 允许 | 上层只关心"操作失败"，不解包具体错误类型 |

```go
// ❌ 错误：%v 丢失错误链，GetError 无法匹配
return nil, fmt.Errorf("create checkout: %v", moduleErrcode.ErrInvalidOrderType)

// ✅ 正确：%w 保留错误链，errors.As 可达
return nil, fmt.Errorf("create checkout: %w", moduleErrcode.ErrInvalidOrderType)
```

**日志级别**：Error（系统异常）/ Warn（关键业务异常）/ Info（关键节点）/ Debug（调试，生产关闭）

**日志规则**：兜底在 Handler 打；带 context（traceID/userID）；禁打敏感信息；外部调用必须记录。

## 5. 请求/响应规范

- 统一响应：`types.Common{Code, Data, Msg}` / `types.CommonList{..., Total, Page, PageSize}`
- 分页 query：`?page=1&page_size=20`
- 请求体用 DTO-Req，带 `json` + `validate` tag
- 每个 Handler 必须 Swagger 注解（详见 §14 及 `api-docs.md`）

## 6. 路由与版本管理

- API 版本前缀：`/api/v1/`、`/api/v2/`，多版本共存
- Dashboard 前缀：`/dashboard/`
- RESTful：资源用复数名词，动作用 HTTP Method
- 路由按业务域分组

## 7. 中间件规范

执行顺序：`Logger → Recovery → CORS → RateLimit → Language → Auth → Handler`

- **全局**：Logger、Recovery、CORS
- **按路由组**：Auth、RateLimit
- **公开接口**（健康检查、webhook）不挂 Auth

## 8. 数据库操作规范

- 所有查询必须透传 context（如 GORM: `db.WithContext(ctx)`）
- 事务使用 ORM 封装的事务回调（如 GORM: `db.Transaction(func(tx) error {...})`），禁手动 Begin/Commit
- 禁 Repo 外直接操作 db
- 批量插入使用 ORM 批量接口（如 GORM: `CreateInBatches`）
- 禁裸 SQL（除非 ORM 无法表达），裸 SQL 必须参数化
- **双 Base Model**：`BaseModel`（无软删除）/ `BaseModelSoftDelete`（有软删除），详见 `api-style.md`

### 8.1 Repo 泛型基础层

推荐使用 `BaseRepo[T]` 泛型架构统一所有 Repo 的 CRUD，消除重复代码：

```go
type IBaseRepo[T any] interface {
    Get(ctx context.Context, opts ...QueryOption) (*T, error)
    List(ctx context.Context, opts ...QueryOption) ([]*T, error)
    Create(ctx context.Context, entity *T) error
    Update(ctx context.Context, entity *T, attrs map[string]interface{}) error
    Delete(ctx context.Context, entity *T) error
    WithSort(sorts ...SortField) QueryOption
    WithForUpdate() QueryOption
}
```

- 业务 Repo 嵌入 `BaseRepo[T]`，仅扩展特定查询方法
- 查询条件通过 `QueryOption` 函数式组合：`s.UserRepo.Get(ctx, s.UserRepo.WithID(id))`
- `WithSort` 基于 GORM tag 反射**白名单防注入**，`sync.Map` 缓存

### 8.2 Repo 多表关联

辅助数据（如多语言翻译）用主 Repo 内跨表查询方法：

```go
// IProductRepo 中定义
ListTranslationsByIdentifiers(ctx context.Context, identifiers []string, locale string) ([]*models.ProductTranslation, error)
```

适用场景：翻译表、配置表等附属数据。核心业务关联仍走 Service 层组装。

## 9. 依赖注入与服务装配

采用 **工厂模式 + 区域策略接口**，集中在 `internal/initializers/`：

- `app.go` — App 结构体 + `NewApp()` 统一入口
- `region_factory.go` — `RegionInitializer` 接口 + `GetRegionInitializer` 工厂
- `region_cn.go` — CNInitializer（微信支付、短信等）
- `region_global.go` — GlobalInitializer（Firebase、Stripe、Gemini 等）

**装配流程**：`NewApp()` → Config → Logger/DB → 通用服务 → `GetRegionInitializer(cfg)` 按区域加载 → 创建 Service

各 cmd（api/dashboard/cron/worker/migration）共享 `NewApp()` 初始化。

## 10. 日志规范

- 通过 `ILogger` 接口抽象，支持 zap / 阿里云日志切换
- 接口定义：`Debug/Info/Warn/Error(msg, ...Field)` + `WithContext(ctx)`
- 开发环境 Debug + Console，生产 Info + JSON
- 统一 `logger.FromContext(ctx)` 获取实例
- 级别划分详见 §4

## 11. 配置管理

- 使用项目选型的配置库加载 `dev_env` 配置
- 敏感信息**禁止硬编码**，必须走环境变量
- 多环境：`dev_env`（本地）/ Docker Compose（测试）/ K8s ConfigMap（生产）
- 区域判断：`cfg.IsCN()` / `cfg.IsUS()`

## 12. 测试规范

- **完整链路**：走 `ServeHTTP`（含中间件），不单测 handler
- **禁 Mock 数据层**：真实 MySQL，仅 Mock 外部系统
- **测试文件**：`test/` 顶层目录
- **表驱动**：`testCases` + `t.Run(tc.name)`，每模块调 `setup()` 初始化
- **GET 断言**：`GetFormatDataFromJSON` 验响应体
- **写操作断言**：响应体 + `GetRecords` 验数据库
- 数据可累加（串联化断言），用 `cmp.Diff` 统一比对
- **边界覆盖**：参数校验 / 鉴权 / 404 / Conflict / 数据库约束
- **覆盖率**：`go test -coverprofile` → 业务逻辑目标 80%
- 完整 Demo 见 `api-test.md`

## 13. 安全规范

- **认证鉴权**：非公开接口必须 Auth 中间件（JWT / Firebase Token）
- **输入校验**：`validate` tag 校验，禁止信任前端数据
- **SQL 注入防护**：GORM 参数化，裸 SQL 用占位符 `?`
- **敏感数据脱敏**：密码/token 禁出现在日志和响应中
- **密码存储**：bcrypt 单向哈希
- **CORS**：生产严格限制 Origin
- **Rate Limit**：高频接口设速率限制

## 14. API 文档

- 每个 Handler 完整 OpenAPI 注解（`@Summary/@Tags/@Param/@Success/@Failure/@Router/@Security`）
- `swag init -g cmd/api/main.go -o docs/`，文档随代码同步
- DTO 字段带 `example` / `enums` 注解
- 引用的结构体**必须核验存在**于 `internal/types`
- 详细示例见 `api-docs.md`

**Swagger 注解中文规范**：

- `@Tags` 使用中文分组（支付 / 产品 / 回调 / 认证 / 用户）
- `@Summary`、`@Description`、`@Param` 描述均使用中文
- `@Success` / `@Failure` 注释使用中文（如 `"创建成功，code=0"`）
- `@Router` 路径必须与 `routers.go` 实际注册路径**交叉校验**，不一致会导致测试 404

## 15. 重构的迭代式方法

- 迭代推进，不可"大爆炸"式修改
- 原子性改动（一步一测），先补测试再重构

## 16. 模块划分原则

- 高内聚低耦合，跨域交互通过 Service Interface，禁直接越界查 Repo

## 17. 技术选型

基于 **Go 语言**，具体框架与库在项目 Plan（`plans/` 目录）的初始需求分析阶段确定，需明确以下维度：

| 维度 | 说明 | 可选方案 |
| --- | --- | --- |
| Go 版本 | 与项目 `go.mod` 一致 | 1.22 / 1.23 / 1.24 |
| HTTP 框架 | 路由与中间件 | go-chi / gin / echo / fiber |
| ORM / 数据访问 | 数据库交互方式 | GORM / sqlx / ent / sqlc |
| 数据库 | 存储引擎 | MySQL / PostgreSQL / SQLite |
| 配置管理 | 配置加载方案 | configor / viper / envconfig |
| 日志 | 日志框架 | zap / slog / zerolog |

**约束**：

- 技术栈一旦在 Plan 确定，项目内统一遵守，不可混用
- 新增中间件/第三方依赖必须封装隔离在适配层
- 选型需与团队技术储备、项目规模匹配

## 18. 核心启示

- 先做对，再做好
- 避免过早抽象
- 按业务逻辑划分模块
- 分步重构，每步验证
- 及时清理无用代码
- 测试是重构的安全网

## 补充参考

- 需求分析规范 (Requirements Analysis Rules) → `.claude/docs/api-analysis.md`
- API 文档生成注释规范 (API Documentation Rules) → `.claude/docs/api-docs.md`
- 代码风格约定规范 (Code Style Rules) → `.claude/docs/api-style.md`
- API 集成测试规则 (API Test Rules) → `.claude/docs/api-test.md`
- 设计系统规范 (Design System - DESIGN.md) → `.claude/docs/design.md`
