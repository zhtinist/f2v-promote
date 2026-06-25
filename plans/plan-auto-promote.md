# Plan: 自动投放策略 (Auto Promote)

> **状态**: 开发中  
> **创建时间**: 2026-03-30  
> **最后更新**: 2026-03-31  
> **关联模块**: `video_stats`, `authors`, `platform_accounts`

---

## 1. 需求概述

### 1.1 业务目标

为每个作者配置投放标准值，系统定时检查 `video_stats` 中的视频数据，满足条件时自动创建投放任务。

### 1.2 投放规则

| 情况 | 条件 | 投放目标 |
|------|------|---------|
| **情况一（投点赞率/推荐）** | 视频最近一次 **单小时播放增量 > 标准值 × 2** | `promotion_target = 推荐` |
| **情况二（投粉丝数/关注）** | 视频最近一次 **单小时播放增量 > 标准值**，或 **（点赞率 > 标准值 且 转发率 > 标准值）** | `promotion_target = 关注` |

> **优先级**：情况一优先于情况二（同时满足时按情况一投放）。

### 1.3 标准值示例

```
大宝见 666（视频号）
  单小时播放量: 2872
  推荐率(点赞率): 1.310%
  评论率: 0.160%
  关注率: 0.713%
  转发率(分享率): 8.099%
  完播率: 17.45%

秦刚（视频号）
  单小时播放量: 549
  推荐率(点赞率): 0.865%
  评论率: 0.127%
  关注率: 1.617%
  转发率(分享率): 5.046%
  完播率: 7.96%
```

---

## 2. 系统架构

### 2.1 服务拆分

系统由 **6 个独立部署的服务** 组成：

| 服务 | 入口 | 部署方式 | 职责 |
|------|------|---------|------|
| **API Server** | `cmd/server` | FC HTTP 触发器 | Web API + 策略管理页面 + 人工确认/拒绝 |
| **Check Order Cron** | `cmd/cron/check-order` ✅ | FC 定时触发器（每分钟） | 轮询订单状态 |
| **Stats Sync Cron** | `cmd/cron/stats` ✅ | FC 定时触发器 | 飞书数据同步 |
| **Promote Cron** | `cmd/cron/promote`（**新增**） | FC 定时触发器（每分钟） | 58s 检测策略 → 创建 log → 飞书通知（可选） |
| **Dispatcher Cron** | `cmd/cron/dispatcher`（**新增**） | FC 定时触发器（每分钟） | 扫描 confirmed 状态的 log → CAS 推 MNS |
| **MNS Consumer** | `cmd/cron/mns-consumer`（**新增**） | FC MNS 触发器 | 幂等消费消息 → 生成标签 → 下单 |

### 2.2 数据流架构

```text
外部服务定期写入 video_stats（含 export_id + 各项指标）
          │
          ▼
┌─── Promote Cron（检测服务）────────────────────────┐
│                                                     │
│  Step 1: 查所有 enabled 策略 → [author_id]          │
│  Step 2: 对每个作者刷新视频列表                       │
│  Step 3: 查 video_stats → 计算指标 → 匹配策略规则    │
│  Step 4: 满足条件 →                                 │
│          ├─ notify_feishu=true  → detected + 飞书推送│
│          └─ notify_feishu=false → confirmed（自动）  │
│                                                     │
└────────────────────┬────────────────────────────────┘
                     │
          ┌──────────┴──────────┐
          ▼                     ▼
   运营在页面「确认投放」    自动确认（直接 confirmed）
   detected → confirmed
   detected → rejected（拒绝）
                     │
                     ▼
┌─── Dispatcher Cron（推送服务）─────────────────────┐
│                                                     │
│  Step 1: 查所有 status=confirmed 的 log             │
│  Step 2: CAS 原子更新 confirmed → queued            │
│  Step 3: 推 MNS 消息 {log_id, queued_at}           │
│  （若推送失败 → 回退 queued → confirmed 下次重试）   │
│                                                     │
└────────────────────┬────────────────────────────────┘
                     │ MNS Message
                     ▼
┌─── MNS Consumer（执行服务）───────────────────────┐
│                                                     │
│  Step 1: CAS 原子更新 queued → running（幂等防护） │
│          └─ 影响行数=0 → 跳过（已被消费）           │
│  Step 2: 从 log 获取 video/author 信息              │
│  Step 3: 调 OpenAI 生成 tag_groups                  │
│  Step 4: 创建 Campaign + Order                      │
│  Step 5: 调 weixin.Client 提交订单                   │
│  Step 6: 更新 log → completed / failed              │
│                                                     │
└─────────────────────────────────────────────────────┘
                     │
                     ▼
          现有 Order Cron 接管轮询订单状态
```

### 2.3 `auto_promote_logs` 状态机

```text
detected ──(人工确认)──→ confirmed ──(Dispatcher CAS)──→ queued ──(Consumer CAS)──→ running ──→ completed
    │                                                                                    └──→ failed
    └──(人工拒绝)──→ rejected

注：notify_feishu=false 时跳过 detected，直接创建为 confirmed
```

| 状态 | 含义 |
|------|------|
| `detected` | Promote Cron 检测命中，等待运营人工确认（仅 notify_feishu=true） |
| `confirmed` | 已确认待投放（人工确认或自动确认），等待 Dispatcher 推 MNS |
| `rejected` | 运营人工拒绝，终态 |
| `queued` | Dispatcher 已推 MNS，等待 Consumer 消费 |
| `running` | Consumer 已锁定，正在执行标签生成/下单 |
| `completed` | 下单成功，已关联 campaign_id / order_id |
| `failed` | 下单失败（OpenAI/诸葛 API 错误等），记录 error_msg |

### 2.4 MNS 可见性与幂等性保障

阿里云 MNS 存在 **消息可见性超时** 问题：消息被消费后若未在超时时间内删除，会重新变为可见被再次消费。

**三层防护**：

1. **Dispatcher CAS 锁**：`UPDATE auto_promote_logs SET status='queued', queued_at=NOW() WHERE id=? AND status='confirmed'` — 原子操作防止同一条 log 被重复推送到 MNS
2. **Consumer CAS 锁**：`UPDATE auto_promote_logs SET status='running' WHERE id=? AND status='queued'` — 原子操作确保只有一个 Consumer 实例执行，影响行数=0 时直接跳过
3. **queued_at 校验**：消息体中携带 `queued_at` 时间戳，Consumer 校验与数据库中的 `queued_at` 一致，防止处理过期消息

---

## 3. 数据库设计

### 3.1 新增表：`author_videos`（作者视频关联表）

建立 `export_id ↔ author_id` 的映射关系，同时缓存视频元数据供投放使用。

```sql
CREATE TABLE author_videos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    author_id BIGINT NOT NULL COMMENT '作者ID，关联 authors.id',
    account_id BIGINT NOT NULL COMMENT '账号ID，关联 platform_accounts.id',
    export_id VARCHAR(256) NOT NULL COMMENT '视频ID，关联 video_stats.export_id',
    description TEXT COMMENT '视频描述',
    cover_url TEXT COMMENT '视频封面',
    publish_time VARCHAR(32) COMMENT '发布时间',
    nonce VARCHAR(256) NOT NULL COMMENT '唯一标识，匹配核心字段',
    raw_data JSON COMMENT 'API原始响应，投放时直接使用',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_nonce (nonce),
    UNIQUE KEY uk_export_id (export_id),
    INDEX idx_author_id (author_id),
    INDEX idx_account_id (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 3.2 新增表：`author_promote_strategies`（作者投放策略配置表）

每个作者一条策略记录，存放标准值阈值。

```sql
CREATE TABLE author_promote_strategies (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    author_id BIGINT NOT NULL COMMENT '作者ID，关联 authors.id',
    account_id BIGINT NOT NULL COMMENT '账号ID，关联 platform_accounts.id',
    platform VARCHAR(32) NOT NULL DEFAULT 'weixin' COMMENT '平台标识',
    enabled TINYINT(1) DEFAULT 1 COMMENT '是否启用自动投放',
    notify_feishu TINYINT(1) DEFAULT 0 COMMENT '是否推送飞书待审核（0=自动确认,1=人工审核）',
    hourly_play_threshold INT NOT NULL COMMENT '单小时播放量标准值',
    like_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '推荐率/点赞率标准值(%)',
    comment_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '评论率标准值(%)',
    follow_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '关注率标准值(%)',
    share_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '转发率/分享率标准值(%)',
    completion_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '完播率标准值(%)',
    last_checked_at DATETIME COMMENT '上次检测时间',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_author_id (author_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 3.3 现有表关系（不改动 `video_stats`）

> `video_stats` 已包含 `platform`、`author_id` 字段，与 `author_videos` 通过 `export_id` 关联。

### 3.4 新增表：`auto_promote_logs`（自动投放执行日志）

记录每次检测→下单的完整生命周期。

```sql
CREATE TABLE auto_promote_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    strategy_id BIGINT NOT NULL COMMENT '策略ID',
    author_id BIGINT NOT NULL COMMENT '作者ID',
    account_id BIGINT NOT NULL COMMENT '账号ID',
    platform VARCHAR(32) NOT NULL DEFAULT 'weixin' COMMENT '平台标识',
    export_id VARCHAR(256) NOT NULL COMMENT '触发投放的视频ID',
    promote_type VARCHAR(20) NOT NULL COMMENT '投放类型: like_rate / followers',
    stat_id_current BIGINT NOT NULL COMMENT '用于计算的最新 video_stats.id',
    stat_id_previous BIGINT COMMENT '用于计算的次新 video_stats.id',
    hourly_play_count INT COMMENT '检测到的单小时播放增量',
    like_rate DECIMAL(6,3) COMMENT '检测到的点赞率(%)',
    share_rate DECIMAL(6,3) COMMENT '检测到的转发率(%)',
    campaign_id BIGINT COMMENT '创建的活动ID',
    order_id BIGINT COMMENT '创建的订单ID',
    status VARCHAR(20) NOT NULL DEFAULT 'detected' COMMENT 'detected/confirmed/rejected/queued/running/completed/failed',
    confirmed_at DATETIME COMMENT '确认时间（人工或自动）',
    queued_at DATETIME COMMENT '推入 MNS 时间（用于幂等校验）',
    error_msg TEXT COMMENT '错误信息',
    video_raw_data JSON COMMENT '投放时使用的视频数据快照',
    author_raw_data JSON COMMENT '投放时使用的作者数据快照',
    tag_groups JSON COMMENT 'OpenAI 生成的标签组',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_stat_current (stat_id_current) COMMENT '同一条 video_stats 数据只能触发一次投放',
    INDEX idx_strategy_id (strategy_id),
    INDEX idx_status (status) COMMENT 'Dispatcher 扫描 confirmed 状态',
    INDEX idx_export_status (export_id, status) COMMENT '并发检查联合索引',
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

---

## 4. 分层实现方案

### 4.1 Model 层

#### `internal/model/author_video.go`（新增）

```go
type AuthorVideo struct {
    Base
    AuthorID    int64          `gorm:"not null;index" json:"author_id"`
    AccountID   int64          `gorm:"not null;index" json:"account_id"`
    ExportID    string         `gorm:"type:varchar(256);not null;uniqueIndex" json:"export_id"`
    Description string         `gorm:"type:text" json:"description"`
    CoverURL    string         `gorm:"type:text" json:"cover_url"`
    PublishTime string         `gorm:"type:varchar(32)" json:"publish_time"`
    Nonce       string         `gorm:"type:varchar(256);not null" json:"nonce"`
    RawData     datatypes.JSON `gorm:"type:json" json:"raw_data"`
}

func (AuthorVideo) TableName() string { return "author_videos" }
```

#### `internal/model/author_promote_strategy.go`（新增）

```go
type AuthorPromoteStrategy struct {
    Base
    AuthorID                int64      `gorm:"not null;uniqueIndex" json:"author_id"`
    AccountID               int64      `gorm:"not null" json:"account_id"`
    Platform                string     `gorm:"type:varchar(32);not null;default:'weixin'" json:"platform"`
    Enabled                 bool       `gorm:"default:true" json:"enabled"`
    NotifyFeishu            bool       `gorm:"default:false" json:"notify_feishu"`
    HourlyPlayThreshold     int        `gorm:"not null" json:"hourly_play_threshold"`
    LikeRateThreshold       float64    `gorm:"type:decimal(6,3);default:0" json:"like_rate_threshold"`
    CommentRateThreshold    float64    `gorm:"type:decimal(6,3);default:0" json:"comment_rate_threshold"`
    FollowRateThreshold     float64    `gorm:"type:decimal(6,3);default:0" json:"follow_rate_threshold"`
    ShareRateThreshold      float64    `gorm:"type:decimal(6,3);default:0" json:"share_rate_threshold"`
    CompletionRateThreshold float64    `gorm:"type:decimal(6,3);default:0" json:"completion_rate_threshold"`
    LastCheckedAt           *time.Time `gorm:"type:datetime" json:"last_checked_at"`
}

func (AuthorPromoteStrategy) TableName() string { return "author_promote_strategies" }
```

#### `internal/model/auto_promote_log.go`（新增）

```go
// 状态常量
const (
    PromoteLogDetected  = "detected"   // 检测命中，待审核（notify_feishu=true）或自动确认
    PromoteLogConfirmed = "confirmed"  // 已确认，等待 Dispatcher 推 MNS
    PromoteLogRejected  = "rejected"   // 人工拒绝（终态）
    PromoteLogQueued    = "queued"     // 已推 MNS，等待 Consumer 消费
    PromoteLogRunning   = "running"    // Consumer 执行中
    PromoteLogCompleted = "completed"  // 下单成功
    PromoteLogFailed    = "failed"     // 下单失败
)

type AutoPromoteLog struct {
    Base
    StrategyID      int64          `gorm:"not null;index" json:"strategy_id"`
    AuthorID        int64          `gorm:"not null" json:"author_id"`
    AccountID       int64          `gorm:"not null" json:"account_id"`
    Platform        string         `gorm:"type:varchar(32);not null;default:'weixin'" json:"platform"`
    ExportID        string         `gorm:"type:varchar(256);not null" json:"export_id"`
    PromoteType     string         `gorm:"type:varchar(20);not null" json:"promote_type"`
    StatIDCurrent   int64          `gorm:"not null;uniqueIndex" json:"stat_id_current"`
    StatIDPrevious  *int64         `json:"stat_id_previous"`
    HourlyPlayCount *int           `json:"hourly_play_count"`
    LikeRate        *float64       `gorm:"type:decimal(6,3)" json:"like_rate"`
    ShareRate       *float64       `gorm:"type:decimal(6,3)" json:"share_rate"`
    CampaignID      *int64         `json:"campaign_id"`
    OrderID         *int64         `json:"order_id"`
    Status          string         `gorm:"type:varchar(20);not null;default:'detected';index" json:"status"`
    ConfirmedAt     *time.Time     `gorm:"type:datetime" json:"confirmed_at"`
    QueuedAt        *time.Time     `gorm:"type:datetime" json:"queued_at"`
    ErrorMsg        *string        `gorm:"type:text" json:"error_msg"`
    VideoRawData    datatypes.JSON `gorm:"type:json" json:"video_raw_data"`
    AuthorRawData   datatypes.JSON `gorm:"type:json" json:"author_raw_data"`
    TagGroups       datatypes.JSON `gorm:"type:json" json:"tag_groups"`
}

func (AutoPromoteLog) TableName() string { return "auto_promote_logs" }
```

---

### 4.2 Repository 层

#### `internal/repository/author_video_repo.go`（新增）

```go
type AuthorVideoRepo struct { db *gorm.DB }

// BulkUpsert 批量按 nonce 插入或更新
func (r *AuthorVideoRepo) BulkUpsert(videos []model.AuthorVideo) (int, error)

// GetByNonce 通过 nonce 反查作者视频
func (r *AuthorVideoRepo) GetByNonce(nonce string) (*model.AuthorVideo, error)

// GetByNonceOrDescription 通过 nonce 或 description 反查作者视频（OR 查询）
func (r *AuthorVideoRepo) GetByNonceOrDescription(nonce, description string) (*model.AuthorVideo, error)

// GetByNoncesOrDescriptions 批量通过 nonce 或 description 反查作者视频（IN 查询）
func (r *AuthorVideoRepo) GetByNoncesOrDescriptions(nonces, descriptions []string) ([]model.AuthorVideo, error)

// GetByExportID 通过视频ID反查作者（保留兼容）
func (r *AuthorVideoRepo) GetByExportID(exportID string) (*model.AuthorVideo, error)

// ListByAuthorID 获取作者的所有视频
func (r *AuthorVideoRepo) ListByAuthorID(authorID int64) ([]model.AuthorVideo, error)

// GetExportIDsByAuthorID 获取作者所有 export_id 列表
func (r *AuthorVideoRepo) GetExportIDsByAuthorID(authorID int64) ([]string, error)
```

#### `internal/repository/strategy_repo.go`（新增）

```go
type StrategyRepo struct { db *gorm.DB }

// ListEnabled 获取所有启用的策略
func (r *StrategyRepo) ListEnabled() ([]model.AuthorPromoteStrategy, error)

// List 分页获取全部策略
func (r *StrategyRepo) List() ([]model.AuthorPromoteStrategy, error)

// GetByID / GetByAuthorID / Create / Update / Delete
// TouchLastChecked 更新最后检测时间
```

#### `internal/repository/auto_promote_log_repo.go`（新增）

```go
type AutoPromoteLogRepo struct { db *gorm.DB }

// Create 创建日志（状态 detected 或 confirmed）
func (r *AutoPromoteLogRepo) Create(log *model.AutoPromoteLog) error

// GetByID 按 ID 查询
func (r *AutoPromoteLogRepo) GetByID(id int64) (*model.AutoPromoteLog, error)

// UpdateStatus 更新状态 + 关联字段
func (r *AutoPromoteLogRepo) UpdateStatus(id int64, status string, updates map[string]any) error

// ConfirmByID 人工确认：detected → confirmed（原子 CAS）
func (r *AutoPromoteLogRepo) ConfirmByID(id int64) (bool, error)

// RejectByID 人工拒绝：detected → rejected（原子 CAS）
func (r *AutoPromoteLogRepo) RejectByID(id int64) (bool, error)

// CASToQueued Dispatcher 原子锁：confirmed → queued + set queued_at（返回影响行数）
func (r *AutoPromoteLogRepo) CASToQueued(id int64) (int64, error)

// CASToRunning Consumer 原子锁：queued → running（返回影响行数，0=已消费）
func (r *AutoPromoteLogRepo) CASToRunning(id int64) (int64, error)

// ListConfirmed 查询所有 status=confirmed 的 log（Dispatcher 扫描用，限制批次大小）
func (r *AutoPromoteLogRepo) ListConfirmed(limit int) ([]model.AutoPromoteLog, error)

// ListDetected 查询所有 status=detected 的 log（前端待审核列表）
func (r *AutoPromoteLogRepo) ListDetected(page, pageSize int) ([]model.AutoPromoteLog, int64, error)

// ExistsByStatIDCurrent 检查某 stat_id_current 是否已存在记录
func (r *AutoPromoteLogRepo) ExistsByStatIDCurrent(statIDCurrent int64) (bool, error)

// HasRecentPromote 检查某 export_id 在 cooldown 时间窗口内是否已有投放记录
func (r *AutoPromoteLogRepo) HasRecentPromote(exportID string, cooldown time.Duration) (bool, error)

// ListByStrategyID 按策略ID查询日志（分页）
func (r *AutoPromoteLogRepo) ListByStrategyID(strategyID int64, page, pageSize int) ([]model.AutoPromoteLog, int64, error)
```

#### `internal/repository/video_stat_repo.go`（改造，新增方法）

```go
// GetLatestTwoByExportID 获取某视频最近两条记录（用于计算播放增量）
func (r *VideoStatRepo) GetLatestTwoByExportID(exportID string) ([]model.VideoStat, error)

// GetLatestByExportIDs 批量获取多个视频的最新记录
func (r *VideoStatRepo) GetLatestByExportIDs(exportIDs []string) ([]model.VideoStat, error)

// UpdateAuthorID 回填单条 video_stats 的 author_id
func (r *VideoStatRepo) UpdateAuthorID(statID int64, authorID int64) error

// BatchUpdateAuthorID 批量回填 author_id（key=statID, value=authorID）
func (r *VideoStatRepo) BatchUpdateAuthorID(updates map[int64]int64) error
```

#### `internal/repository/author_repo.go`（已存在，确认方法）

```go
// GetByID 按主键查询单个作者
func (r *AuthorRepo) GetByID(id int64) (*model.Author, error)

// ListAll 获取所有作者（供 AuthorVideoMatchService P2 遍历使用）
func (r *AuthorRepo) ListAll() ([]model.Author, error)
```

---

### 4.3 MNS 客户端（新增）

#### `internal/pkg/mns/client.go`

接入阿里云 MNS（消息服务）。

```go
type MNSClient struct {
    endpoint    string
    accessKeyID string
    accessSecret string
    queueName   string
}

// SendMessage 发送消息到队列
func (c *MNSClient) SendMessage(ctx context.Context, body []byte) (string, error)

// ReceiveMessage 接收并删除消息（FC MNS 触发器模式下由 FC 自动调用，不需要手动接收）
```

**MNS 消息体定义**：

```go
// PromoteMessage MNS 推送的消息体
// Dispatcher 推送时填充 QueuedAt，Consumer 校验一致性
type PromoteMessage struct {
    PromoteLogID int64     `json:"promote_log_id"` // auto_promote_logs.id
    QueuedAt     time.Time `json:"queued_at"`       // 推入 MNS 的时间戳，用于幂等校验
}
```

#### 配置项

```go
// Config 新增
MNSEndpoint       string // 阿里云 MNS 接入点
MNSAccessKeyID    string
MNSAccessKeySecret string
MNSQueueName      string // 队列名称，如 "auto-promote-queue"
```

---

### 4.4 Service 层

#### `internal/service/promote_detector.go`（改造 — Promote Cron 使用）

只负责**检测 + 创建 log（detected/confirmed） + 飞书通知（可选）**，不推 MNS、不做下单。

```go
type PromoteDetectorService struct {
    strategyRepo       *repository.StrategyRepo
    authorVideoRepo    *repository.AuthorVideoRepo
    videoStatRepo      *repository.VideoStatRepo
    promoteLogRepo     *repository.AutoPromoteLogRepo
    authorRepo         *repository.AuthorRepo
    weixinClient       *weixin.Client
    notifier           *NotifierService  // 飞书通知（复用现有 NotifierService）
    cfg                *config.Config
}
```

**决策 DTO**：

```go
// PromoteDecision 单个视频的投放决策结果
type PromoteDecision struct {
    ExportID       string  // 视频 ID
    PromoteType    string  // "like_rate" 或 "followers"
    StatIDCurrent  int64   // 本次使用的最新 video_stats.id
    StatIDPrevious *int64  // 本次使用的次新 video_stats.id（仅 1 条记录时为 nil）
    HourlyPlay     int     // 计算得到的单小时播放增量
    LikeRate       float64 // 计算得到的点赞率
    ShareRate      float64 // 计算得到的转发率
    VideoRawData   []byte  // author_videos.raw_data 快照
    AuthorRawData  []byte  // authors 数据快照
}
```

**核心方法**：

```go
const (
    promoteExecTimeout  = 58 * time.Second // 单次执行超时（FC 每分钟触发，预留 2s）
    promotePollInterval = 10 * time.Second  // 轮询间隔
    maxConcurrency      = 5                 // 最大并发作者数（防止打爆诸葛 API）
    videoRefreshMinAge  = 5 * time.Minute   // 视频列表刷新最小间隔（避免频繁调诸葛 API）
    defaultVideoCooldown = 1 * time.Minute  // 同视频投放冷却时间（可通过配置覆盖）
)

// Run 检测主入口（Cron 每分钟调一次，58s 内循环）
func (s *PromoteDetectorService) Run() string {
    ctx, cancel := context.WithTimeout(context.Background(), promoteExecTimeout)
    defer cancel()

    var mu sync.Mutex
    totalChecked, totalTriggered := 0, 0
    round := 0

    for {
        select {
        case <-ctx.Done():
            return s.summary("timeout", round, totalChecked, totalTriggered)
        default:
        }

        round++

        // 1. 查所有 enabled 策略
        strategies, err := s.strategyRepo.ListEnabled()
        if err != nil {
            log.Printf("[promote] list strategies error: %v", err)
            // 等待下一轮重试
            select {
            case <-ctx.Done():
                return s.summary("error", round, totalChecked, totalTriggered)
            case <-time.After(promotePollInterval):
                continue
            }
        }

        // 2. 并发处理各作者策略（不同作者间无数据依赖）
        g, gCtx := errgroup.WithContext(ctx)
        sem := make(chan struct{}, maxConcurrency) // 信号量控制并发数

        for _, strategy := range strategies {
            strategy := strategy // capture loop var

            g.Go(func() error {
                // 获取信号量
                select {
                case sem <- struct{}{}:
                    defer func() { <-sem }()
                case <-gCtx.Done():
                    return gCtx.Err()
                }

                // 2a. 刷新作者视频列表（仅 last_checked_at 超过 videoRefreshMinAge 时才刷新）
                if strategy.LastCheckedAt == nil || time.Since(*strategy.LastCheckedAt) > videoRefreshMinAge {
                    if err := s.refreshAuthorVideos(gCtx, strategy); err != nil {
                        log.Printf("[promote] refresh videos for author %d error: %v", strategy.AuthorID, err)
                        // 刷新失败不阻断，继续用已有视频列表评估
                    }
                }

                // 2b. 获取作者所有 export_id → 查 video_stats → 计算指标 → 匹配策略
                decisions, err := s.evaluateVideos(gCtx, strategy)
                if err != nil {
                    log.Printf("[promote] evaluate videos for author %d error: %v", strategy.AuthorID, err)
                    return nil // 单个作者失败不影响其他作者
                }

                // 2c. 对每个命中的视频
                for _, d := range decisions {
                    mu.Lock()
                    totalChecked++
                    mu.Unlock()

                    // 并发去重：检查 stat_id_current 是否已被处理
                    // → DB 唯一索引 uk_stat_current 兜底
                    exists, _ := s.promoteLogRepo.ExistsByStatIDCurrent(d.StatIDCurrent)
                    if exists {
                        continue // 同一份数据增量已处理过
                    }

                    // 视频级冷却：同一 export_id 在 cooldown 时间窗口内不重复投放
                    cooldown := time.Duration(s.cfg.AutoPromoteVideoCooldownSec) * time.Second
                    if cooldown <= 0 {
                        cooldown = defaultVideoCooldown
                    }
                    recent, _ := s.promoteLogRepo.HasRecentPromote(d.ExportID, cooldown)
                    if recent {
                        continue // 冷却期内，跳过
                    }

                    // 根据 notify_feishu 决定初始状态
                    initialStatus := model.PromoteLogConfirmed // 默认自动确认
                    if strategy.NotifyFeishu {
                        initialStatus = model.PromoteLogDetected // 需要人工审核
                    }

                    // 创建 auto_promote_log
                    promoteLog := buildPromoteLog(d, strategy, initialStatus)
                    if initialStatus == model.PromoteLogConfirmed {
                        now := time.Now()
                        promoteLog.ConfirmedAt = &now // 自动确认记录时间
                    }
                    if err := s.promoteLogRepo.Create(promoteLog); err != nil {
                        log.Printf("[promote] create log for %s error: %v", d.ExportID, err)
                        continue // 唯一索引冲突 = 已被另一次执行处理
                    }

                    // 若开启飞书通知 → 推消息到飞书
                    if strategy.NotifyFeishu && s.notifier != nil {
                        msg := fmt.Sprintf("🎯 自动投放检测命中\n视频: %s\n类型: %s\n播放增量: %d\n请前往管理页面确认投放",
                            d.ExportID, d.PromoteType, d.HourlyPlay)
                        s.notifier.Send(msg)
                    }

                    mu.Lock()
                    totalTriggered++
                    mu.Unlock()
                }

                // 更新策略最后检测时间
                s.strategyRepo.TouchLastChecked(strategy.ID)
                return nil // 单个作者失败不影响其他作者（不 return error）
            })
        }

        g.Wait() // 等待本轮所有策略处理完成

        // 等待下一轮
        select {
        case <-ctx.Done():
            return s.summary("done", round, totalChecked, totalTriggered)
        case <-time.After(promotePollInterval):
        }
    }
}

// refreshAuthorVideos 刷新单个作者的视频列表到 author_videos
func (s *PromoteDetectorService) refreshAuthorVideos(ctx context.Context, strategy model.AuthorPromoteStrategy) error

// evaluateVideos 评估作者所有视频是否满足投放条件
// 返回的 PromoteDecision 包含 StatIDCurrent / StatIDPrevious
// 边界：若某视频仅有 1 条 video_stats 记录，则只计算率指标（likeRate/shareRate），
//       hourlyPlay 设为 0（无法计算增量），StatIDPrevious 为 nil
func (s *PromoteDetectorService) evaluateVideos(ctx context.Context, strategy model.AuthorPromoteStrategy) ([]PromoteDecision, error)
```

#### `internal/service/author_video_match.go`（已实现 — video_stats 写入时自动关联 author_id）

在 `video_stats` 数据写入时，自动为缺少 `author_id` 的记录关联作者。

```go
type AuthorVideoMatchService struct {
    authorVideoRepo *repository.AuthorVideoRepo
    videoStatRepo   *repository.VideoStatRepo
    authorRepo      *repository.AuthorRepo
    weixinClient    *weixin.Client
}
```

**核心流程**：

```
video_stats 写入 (BulkUpsert 完成后)
    │
    └── FillAuthorIDs 批量预加载 + 内存匹配
         │
         ├── P1: 批量查 author_videos 表 (nonce IN ? OR description IN ?)
         │    └── 构建 nonceMap + descMap → 内存匹配
         │    └── ✅ 命中 → BatchUpdateAuthorID 回填
         │
         └── P2: 未命中的走 API 聚合匹配 (batchMatchByAPI)
              │  计算最宽日期范围，每个作者只调一次 API
              │  结果落库 author_videos（缓存供 P1 后续命中）
              │
              ├── ✅ nonce/description 匹配到 → BatchUpdateAuthorID 回填
              └── ❌ 未匹配 → author_id 保持 NULL
```

**核心方法**：

```go
// FillAuthorIDs 批量为 video_stats 回填 author_id（批量预加载 + 内存匹配）
func (s *AuthorVideoMatchService) FillAuthorIDs(stats []model.VideoStat) {
    if len(stats) == 0 {
        return
    }

    // ── 收集待匹配的 nonce / description ──
    nonces := make([]string, 0, len(stats))
    descs := make([]string, 0, len(stats))
    for _, stat := range stats {
        if stat.AuthorID != nil && *stat.AuthorID != 0 {
            continue
        }
        if stat.Nonce != "" {
            nonces = append(nonces, stat.Nonce)
        }
        if stat.Description != "" {
            descs = append(descs, strings.TrimSpace(stat.Description))
        }
    }

    // ── P1: 批量查 author_videos 表，构建 nonceMap + descMap ──
    nonceMap, descMap := s.buildAuthorVideoMaps(nonces, descs)

    // ── 内存匹配 + 收集未命中列表 ──
    updates := make(map[int64]int64) // statID → authorID
    var unmatchedStats []model.VideoStat

    for i, stat := range stats {
        if stat.AuthorID != nil && *stat.AuthorID != 0 {
            continue
        }
        // nonce 优先匹配
        if stat.Nonce != "" {
            if authorID, ok := nonceMap[stat.Nonce]; ok {
                stats[i].AuthorID = &authorID
                updates[stat.ID] = authorID
                continue
            }
        }
        // description 匹配
        if stat.Description != "" {
            if authorID, ok := descMap[strings.TrimSpace(stat.Description)]; ok {
                stats[i].AuthorID = &authorID
                updates[stat.ID] = authorID
                continue
            }
        }
        unmatchedStats = append(unmatchedStats, stats[i])
    }

    // ── P2: 未命中的走 API 聚合匹配 ──
    if len(unmatchedStats) > 0 {
        apiMatches := s.batchMatchByAPI(unmatchedStats)
        for statID, authorID := range apiMatches {
            updates[statID] = authorID
        }
    }

    // ── 批量回填 DB ──
    if len(updates) > 0 {
        s.videoStatRepo.BatchUpdateAuthorID(updates)
    }
}

// batchMatchByAPI 聚合 API 调用：计算最宽日期范围，每个作者只调一次 API
//
// 流程：
//   1. 计算所有未匹配 stats 的 publishDate 最宽范围 (±1天)
//   2. 获取所有作者列表 (authorRepo.ListAll)
//   3. 遍历每个作者，调 weixinClient.GetAuthorVideos(accountID, username, startDate, endDate)
//   4. 全部 API 结果落库 author_videos（缓存供后续 P1 命中）
//   5. 构建 apiNonceMap + apiDescMap，对未匹配 stats 做内存匹配
func (s *AuthorVideoMatchService) batchMatchByAPI(stats []model.VideoStat) map[int64]int64
```

> **调用时机**：在 `StatsSyncService.Run()` 中作为前置步骤执行（查询 `author_id IS NULL` 的记录，调用 `FillAuthorIDs`）。

#### `internal/service/promote_dispatcher.go`（新增 — Dispatcher Cron 使用）

只负责**扫描 confirmed 状态 → CAS 推 MNS → 失败回退**。

```go
type PromoteDispatcherService struct {
    promoteLogRepo *repository.AutoPromoteLogRepo
    mnsClient      *mns.MNSClient
    cfg            *config.Config
}

// Run Dispatcher 主入口（Cron 每分钟调一次）
func (s *PromoteDispatcherService) Run() string {
    ctx, cancel := context.WithTimeout(context.Background(), 58*time.Second)
    defer cancel()

    totalDispatched := 0
    for {
        select {
        case <-ctx.Done():
            return fmt.Sprintf("dispatcher done: dispatched=%d", totalDispatched)
        default:
        }

        // 1. 查所有 status=confirmed 的 log（限制批次大小）
        logs, err := s.promoteLogRepo.ListConfirmed(50)
        if err != nil || len(logs) == 0 {
            select {
            case <-ctx.Done():
                return fmt.Sprintf("dispatcher done: dispatched=%d", totalDispatched)
            case <-time.After(10 * time.Second):
                continue
            }
        }

        for _, log := range logs {
            // 2. CAS 原子更新 confirmed → queued + set queued_at
            affected, err := s.promoteLogRepo.CASToQueued(log.ID)
            if err != nil || affected == 0 {
                continue // 已被其他实例处理或状态已变
            }

            // 3. 获取更新后的 log（含 queued_at）
            updatedLog, _ := s.promoteLogRepo.GetByID(log.ID)
            queuedAt := time.Now()
            if updatedLog != nil && updatedLog.QueuedAt != nil {
                queuedAt = *updatedLog.QueuedAt
            }

            // 4. 推 MNS 消息 {promote_log_id, queued_at}
            msgBody, _ := json.Marshal(mns.PromoteMessage{
                PromoteLogID: log.ID,
                QueuedAt:     queuedAt,
            })
            if _, err := s.mnsClient.SendMessage(ctx, msgBody); err != nil {
                // 推送失败 → 回退 queued → confirmed（下次重试）
                s.promoteLogRepo.UpdateStatus(log.ID, model.PromoteLogConfirmed, map[string]any{
                    "queued_at": nil,
                })
                continue
            }

            totalDispatched++
        }

        select {
        case <-ctx.Done():
            return fmt.Sprintf("dispatcher done: dispatched=%d", totalDispatched)
        case <-time.After(5 * time.Second):
        }
    }
}
```

#### `internal/service/promote_executor.go`（改造 — MNS Consumer 使用）

只负责**幂等消费消息 → 生成标签 → 创建订单 → 提交诸葛**。

```go
type PromoteExecutorService struct {
    promoteLogRepo *repository.AutoPromoteLogRepo
    campaignRepo   *repository.CampaignRepo
    orderRepo      *repository.OrderRepo
    authorRepo     *repository.AuthorRepo
    weixinClient   *weixin.Client
    openAI         *OpenAIService
    rateLimiter    *RateLimiter
    cfg            *config.Config
}
```

**核心方法**：

```go
// Execute 消费单条 MNS 消息（幂等防护）
func (s *PromoteExecutorService) Execute(ctx context.Context, msg mns.PromoteMessage) error {
    // 1. CAS 原子更新 queued → running（幂等防护第二层）
    affected, err := s.promoteLogRepo.CASToRunning(msg.PromoteLogID)
    if err != nil {
        return fmt.Errorf("cas to running: %w", err)
    }
    if affected == 0 {
        // 已被其他 Consumer 实例处理，跳过
        return nil
    }

    // 2. 查 auto_promote_log → 校验 queued_at 一致性
    promoteLog, err := s.promoteLogRepo.GetByID(msg.PromoteLogID)
    if err != nil || promoteLog == nil {
        return fmt.Errorf("invalid promote log: %d", msg.PromoteLogID)
    }
    if promoteLog.QueuedAt != nil && !promoteLog.QueuedAt.Equal(msg.QueuedAt) {
        // 消息版本不一致，可能是过期的 MNS 重复消息
        return nil
    }

    // 3. defer: 任何 panic/error 都标记为 failed
    var execErr error
    defer func() {
        if execErr != nil {
            errMsg := execErr.Error()
            s.promoteLogRepo.UpdateStatus(promoteLog.ID, model.PromoteLogFailed, map[string]any{
                "error_msg": errMsg,
            })
        }
    }()

    // 4. 从 log 获取 video/author raw_data
    // 5. 调 OpenAI 生成 tag_groups
    tagGroups, execErr := s.openAI.GenerateZhugeTags(script, flatTags, tagGroupCount)
    if execErr != nil {
        return execErr
    }

    // 6. 创建 Campaign + Order
    campaign, execErr := s.campaignRepo.Create(...)
    if execErr != nil {
        return execErr
    }
    order, execErr := s.orderRepo.CreateZhuge(...)
    if execErr != nil {
        return execErr
    }

    // 7. 调诸葛 CreatePlan
    s.rateLimiter.WaitIfNeeded()
    _, _, execErr = s.weixinClient.CreatePlan(strconv.FormatInt(promoteLog.AccountID, 10), planData)
    if execErr != nil {
        return execErr
    }

    // 8. 全部成功 → completed
    execErr = nil
    s.promoteLogRepo.UpdateStatus(promoteLog.ID, model.PromoteLogCompleted, map[string]any{
        "campaign_id": campaign.ID,
        "order_id":    order.ID,
        "tag_groups":  tagGroupsJSON,
    })
    return nil
}
```

---

### 4.5 核心算法：指标计算（不变）

#### 单小时播放增量

```go
// 取 video_stats 中该 export_id 最近两条记录（按 created_at DESC 排序）
// records[0] = 最新, records[1] = 次新
//
// 注意：VideoStat.CreatedAt 是 string 类型 "2006-01-02 15:04:05"
// 需先 time.Parse(time.DateTime, records[x].CreatedAt) 转为 time.Time
//
// t0, _ := time.Parse(time.DateTime, records[0].CreatedAt)
// t1, _ := time.Parse(time.DateTime, records[1].CreatedAt)
// hours := math.Max(t0.Sub(t1).Hours(), 1)
// hourlyPlayCount = int(float64(records[0].PlayCount - records[1].PlayCount) / hours)
```

#### 各项率指标

```go
// likeRate    = LikeCount / PlayCount * 100
// shareRate   = ShareCount / PlayCount * 100
```

#### 策略匹配

```go
func matchStrategy(hourlyPlay int, likeRate, shareRate float64, s model.AuthorPromoteStrategy) string {
    // 情况一优先：单小时播放 > 标准值×2 → 投点赞率(推荐)
    if hourlyPlay > s.HourlyPlayThreshold * 2 {
        return "like_rate"
    }
    // 情况二：单小时播放 > 标准值，或（点赞率 > 标准 且 转发率 > 标准）→ 投粉丝(关注)
    if hourlyPlay > s.HourlyPlayThreshold ||
       (likeRate > s.LikeRateThreshold && shareRate > s.ShareRateThreshold) {
        return "followers"
    }
    return "" // 不投放
}
```

---

### 4.6 Handler 层

#### `internal/handler/v1/strategy.go`（新增）

策略 CRUD + 执行日志查看。

```go
type StrategyHandler struct {
    strategyRepo *repository.StrategyRepo
    logRepo      *repository.AutoPromoteLogRepo
}

// List    GET    /strategies              - 策略列表
// Create  POST   /strategies              - 创建策略
// Update  PUT    /strategies/:id          - 修改策略
// Delete  DELETE /strategies/:id          - 删除策略
// Logs    GET    /strategies/:id/logs     - 查看执行日志
// Pending GET    /strategies/pending      - 待审核列表（detected 状态）
// Confirm POST   /strategies/logs/:id/confirm - 人工确认投放
// Reject  POST   /strategies/logs/:id/reject  - 人工拒绝投放
```

### 4.7 Router 注册

`internal/handler/router.go` 新增：

```go
// Router 结构体添加字段
Strategy *v1.StrategyHandler

// RegisterRoutes 中追加
strategies := engine.Group("/strategies")
strategies.Use(authMW)
{
    strategies.GET("", r.Strategy.List)
    strategies.POST("", r.Strategy.Create)
    strategies.PUT("/:id", r.Strategy.Update)
    strategies.DELETE("/:id", r.Strategy.Delete)
    strategies.GET("/:id/logs", r.Strategy.Logs)
    strategies.GET("/pending", r.Strategy.Pending)
    strategies.POST("/logs/:id/confirm", r.Strategy.Confirm)
    strategies.POST("/logs/:id/reject", r.Strategy.Reject)
}
```

---

## 5. 服务入口

### 5.1 `cmd/cron/promote/main.go`（改造）

```go
func main() {
    cfg := config.Load()
    db := initDB(cfg)
    notifier := service.NewNotifierService(cfg.WebhookURL, cfg.AppName)

    detector := service.NewPromoteDetectorService(
        repository.NewStrategyRepo(db),
        repository.NewAuthorVideoRepo(db),
        repository.NewVideoStatRepo(db),
        repository.NewAutoPromoteLogRepo(db),
        repository.NewAuthorRepo(db),
        weixin.NewClient(cfg, repository.NewPlatformAccountRepo(db), repository.NewZhugeTagRepo(db)),
        notifier, // 飞书通知（可选）
        cfg,
    )

    result := detector.Run()
    fmt.Println(result)
}
```

### 5.2 `cmd/cron/dispatcher/main.go`（新增）

```go
func main() {
    cfg := config.Load()
    db := initDB(cfg)
    mnsClient := mns.NewClient(cfg)

    dispatcher := service.NewPromoteDispatcherService(
        repository.NewAutoPromoteLogRepo(db),
        mnsClient,
        cfg,
    )

    result := dispatcher.Run()
    fmt.Println(result)
}
```

### 5.3 `cmd/cron/mns-consumer/main.go`（改造）

```go
func main() {
    fc.Start(func(ctx context.Context, event []byte) (string, error) {
        var msg mns.PromoteMessage
        json.Unmarshal(event, &msg)

        executor := buildExecutor()
        err := executor.Execute(ctx, msg) // 传入完整 PromoteMessage（含 queued_at）
        if err != nil {
            return "error", err
        }
        return "ok", nil
    })
}
```

### 5.4 部署配置

#### `s.cron-promote.prod.yaml`（新增）

```yaml
edition: 3.0.0
name: f2v-promote-cron-promote
resources:
  f2v-promote-cron-promote:
    component: fc3
    props:
      region: cn-hangzhou
      functionName: f2v-promote-cron-promote
      runtime: custom
      handler: index.handler
      timeout: 120
      triggers:
        - triggerName: timer
          triggerType: timer
          triggerConfig:
            cronExpression: "0 * * * * *"  # 每分钟
            enable: true
```

#### `s.mns-consumer.prod.yaml`（新增）

```yaml
edition: 3.0.0
name: f2v-promote-mns-consumer
resources:
  f2v-promote-mns-consumer:
    component: fc3
    props:
      region: cn-hangzhou
      functionName: f2v-promote-mns-consumer
      runtime: custom
      handler: index.handler
      timeout: 300
      triggers:
        - triggerName: mns-trigger
          triggerType: eventbridge  # 通过 EventBridge 对接 MNS Queue
          triggerConfig:
            triggerEnable: true
            asyncInvocationType: false
            eventSourceConfig:
              eventSourceType: MNS
              eventSourceParameters:
                sourceMNSParameters:
                  queueName: auto-promote-queue
                  isBase64Decode: true
```

#### `s.cron-dispatcher.prod.yaml`（新增）

```yaml
edition: 3.0.0
name: f2v-promote-cron-dispatcher
resources:
  f2v-promote-cron-dispatcher:
    component: fc3
    props:
      region: cn-hangzhou
      functionName: f2v-promote-cron-dispatcher
      runtime: custom
      handler: index.handler
      timeout: 120
      triggers:
        - triggerName: timer
          triggerType: timer
          triggerConfig:
            cronExpression: "0 * * * * *"  # 每分钟
            enable: true
```

---

## 6. 防重复投放机制

### 6.1 基于 `stat_id_current` 的数据级去重

核心问题：Promote Cron 每分钟执行一次，两次间隔可能扫描到**同一份 `video_stats` 数据增量**（即最新两条记录未变化），导致重复下单。

**解决方案**：`auto_promote_logs.stat_id_current` 设为 **UNIQUE KEY**。

- `stat_id_current` = 本次判断使用的最新 `video_stats.id`
- `stat_id_previous` = 本次判断使用的次新 `video_stats.id`
- 创建 `auto_promote_log` 前先查是否已存在同 `stat_id_current` 的记录
- DB 唯一索引 `uk_stat_current` 作为最终兜底（即使应用层并发竞争，INSERT 冲突也不会重复）

**效果**：同一条 `video_stats` 数据只会触发一次投放决策，即使 Cron 多次扫描到。

### 6.2 58s 超时循环

与现有 Order Cron 一致，Promote Cron 采用 `context.WithTimeout(58s)` 循环模式：

```text
FC 每分钟触发 → 58s 内循环检测 → 超时退出
           └→ 每 10s 一轮：查策略 → 刷视频 → 评估 → 创建 log
```

### 6.3 视频级冷却机制

同一视频（`export_id`）在冷却时间窗口内不会重复投放：

- 检测命中后，先查 `auto_promote_logs` 中该 `export_id` 最近一条记录的 `created_at`
- 若 `now - created_at < cooldown` 则跳过（不管该记录状态是 detected/confirmed/queued/running/completed/failed）
- 冷却时间通过配置项 `AutoPromoteVideoCooldownSec` 控制，默认 **60秒**
- 冷却过后，若视频仍然满足条件且产生了新的 `video_stats` 数据，则允许再次投放

```text
检测命中视频A
  │
  ├── stat_id_current 已存在？ → skip（数据级去重）
  ├── export_id 冷却期内？ → skip（视频级冷却）
  └── 通过 → 创建 auto_promote_log (detected/confirmed)
```

---

## 7. 开发任务清单

### Phase 1: 数据层（Model + Repository + 迁移）

| # | 任务 | 文件 |
|---|------|------|
| 1.1 | 创建 `AuthorVideo` Model | `internal/model/author_video.go` |
| 1.2 | 创建 `AuthorPromoteStrategy` Model | `internal/model/author_promote_strategy.go` |
| 1.3 | 创建 `AutoPromoteLog` Model（含状态常量） | `internal/model/auto_promote_log.go` |
| 1.4 | 创建 `AuthorVideoRepo`（新增 nonce/description 批量查询） | `internal/repository/author_video_repo.go` |
| 1.5 | 创建 `StrategyRepo` | `internal/repository/strategy_repo.go` |
| 1.6 | 创建 `AutoPromoteLogRepo` | `internal/repository/auto_promote_log_repo.go` |
| 1.7 | `VideoStatRepo` 新增增量查询方法 | `internal/repository/video_stat_repo.go` |
| 1.8 | 确认 `AuthorRepo` 已有 `GetByID` / `ListAll` | `internal/repository/author_repo.go` |
| 1.9 | SQL 迁移脚本 | `mysql/migration_auto_promote.sql` |

### Phase 2: MNS 客户端

| # | 任务 | 文件 |
|---|------|------|
| 2.1 | 阿里云 MNS 客户端封装 | `internal/pkg/mns/client.go` |
| 2.2 | MNS 消息体定义 | `internal/pkg/mns/message.go` |
| 2.3 | Config 新增 MNS 相关配置项 | `internal/config/config.go` |

### Phase 3: Service 层

| # | 任务 | 文件 | 说明 |
|---|------|------|------|
| 3.1 | 改造 `PromoteDetectorService` | `internal/service/promote_detector.go` | 不再推 MNS；根据 notify_feishu 创建 detected/confirmed + 飞书通知 |
| 3.2 | 新增 `PromoteDispatcherService` | `internal/service/promote_dispatcher.go` | 扫描 confirmed → CAS queued → 推 MNS（含失败回退） |
| 3.3 | 改造 `PromoteExecutorService` | `internal/service/promote_executor.go` | CAS queued→running 幂等防护 + queued_at 校验 |
| 3.4 | MNS 消息体新增 `queued_at` | `internal/pkg/mns/message.go` | Consumer 校验消息版本 |

### Phase 4: Handler + Router

| # | 任务 | 文件 | 说明 |
|---|------|------|------|
| 4.1 | 改造 `StrategyHandler` | `internal/handler/v1/strategy.go` | 新增 notify_feishu 字段支持 |
| 4.2 | 新增确认/拒绝接口 | `internal/handler/v1/strategy.go` | `POST /strategies/logs/:id/confirm` + `reject` |
| 4.3 | 新增待审核列表接口 | `internal/handler/v1/strategy.go` | `GET /strategies/pending` 返回 detected 状态列表 |
| 4.4 | Router 注册新路由 | `internal/handler/router.go` | 确认/拒绝/待审核路由 |
| 4.5 | `main.go` 依赖注入 | `cmd/server/main.go` | 注入新增依赖 |

### Phase 5: 前端页面

| # | 任务 | 文件 | 说明 |
|---|------|------|------|
| 5.1 | 改造策略管理页面 | `templates/strategies.html` | 新增 notify_feishu 开关 + 待审核 Tab + 确认/拒绝按钮 |
| 5.2 | `PageHandler.StrategiesPage` | `internal/handler/v1/page.go` | 已有，无需改动 |
| 5.3 | Router 页面路由 | `internal/handler/router.go` | 已有，无需改动 |
| 5.4 | 侧边栏导航 | `templates/base.html` | 已有，无需改动 |

**页面功能强化**：

- **策略列表**：新增「飞书审核」列，展示 notify_feishu 开关状态
- **创建/编辑弹窗**：新增「开启飞书审核」开关
- **待审核 Tab**：展示 detected 状态的日志，提供「确认投放」/「拒绝」按钮
- **投放日志 Tab**：增加 confirmed/queued/running 状态展示

### Phase 6: 服务入口 + 部署

| # | 任务 | 文件 |
|---|------|------|
| 6.1 | 改造 Promote Cron 入口 | `cmd/cron/promote/main.go` |
| 6.2 | 新增 Dispatcher Cron 入口 | `cmd/cron/dispatcher/main.go` |
| 6.3 | 改造 MNS Consumer 入口 | `cmd/cron/mns-consumer/main.go` |
| 6.4 | Dispatcher Cron 部署配置 | `s.cron-dispatcher.prod.yaml` |

### Phase 7: 数据迁移 + Model 更新

| # | 任务 | 文件 |
|---|------|------|
| 7.1 | `AuthorPromoteStrategy` 新增 `notify_feishu` | `internal/model/author_promote_strategy.go` |
| 7.2 | `AutoPromoteLog` 新增状态/字段 | `internal/model/auto_promote_log.go` |
| 7.3 | `AutoPromoteLogRepo` 新增 CAS/确认/拒绝方法 | `internal/repository/auto_promote_log_repo.go` |
| 7.4 | 更新 SQL 迁移脚本 | `mysql/migration_auto_promote.sql` |

### Phase 8: 测试

| # | 任务 | 文件 |
|---|------|------|
| 8.1 | 策略 CRUD + 确认/拒绝接口测试 | `internal/handler/v1/strategy_test.go` |
| 8.2 | Detector 检测 + 自动/手动确认逻辑测试 | `internal/service/promote_detector_test.go` |
| 8.3 | Dispatcher CAS 幂等测试 | `internal/service/promote_dispatcher_test.go` |
| 8.4 | Executor CAS 幂等测试 | `internal/service/promote_executor_test.go` |

---

## 8. SQL 迁移脚本汇总

```sql
-- mysql/migration_auto_promote.sql

-- 作者视频关联表
CREATE TABLE IF NOT EXISTS author_videos (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    author_id BIGINT NOT NULL COMMENT '作者ID',
    account_id BIGINT NOT NULL COMMENT '账号ID',
    export_id VARCHAR(256) NOT NULL COMMENT '视频ID',
    description TEXT,
    cover_url TEXT,
    publish_time VARCHAR(32),
    nonce VARCHAR(256) NOT NULL COMMENT '唯一标识，匹配核心字段',
    raw_data JSON,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_nonce (nonce),
    UNIQUE KEY uk_export_id (export_id),
    INDEX idx_author_id (author_id),
    INDEX idx_account_id (account_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 作者投放策略配置表
CREATE TABLE IF NOT EXISTS author_promote_strategies (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    author_id BIGINT NOT NULL COMMENT '作者ID',
    account_id BIGINT NOT NULL COMMENT '账号ID',
    platform VARCHAR(32) NOT NULL DEFAULT 'weixin' COMMENT '平台标识',
    enabled TINYINT(1) DEFAULT 1,
    notify_feishu TINYINT(1) DEFAULT 0 COMMENT '是否推送飞书待审核（0=自动确认,1=人工审核）',
    hourly_play_threshold INT NOT NULL COMMENT '单小时播放量标准值',
    like_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '点赞率标准值(%)',
    comment_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '评论率标准值(%)',
    follow_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '关注率标准值(%)',
    share_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '转发率/分享率标准值(%)',
    completion_rate_threshold DECIMAL(6,3) DEFAULT 0 COMMENT '完播率标准值(%)',
    last_checked_at DATETIME,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_author_id (author_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 自动投放执行日志表
CREATE TABLE IF NOT EXISTS auto_promote_logs (
    id BIGINT AUTO_INCREMENT PRIMARY KEY,
    strategy_id BIGINT NOT NULL,
    author_id BIGINT NOT NULL,
    account_id BIGINT NOT NULL,
    platform VARCHAR(32) NOT NULL DEFAULT 'weixin' COMMENT '平台标识',
    export_id VARCHAR(256) NOT NULL,
    promote_type VARCHAR(20) NOT NULL COMMENT 'like_rate / followers',
    stat_id_current BIGINT NOT NULL COMMENT '用于计算的最新 video_stats.id',
    stat_id_previous BIGINT COMMENT '用于计算的次新 video_stats.id',
    hourly_play_count INT,
    like_rate DECIMAL(6,3),
    share_rate DECIMAL(6,3),
    campaign_id BIGINT,
    order_id BIGINT,
    status VARCHAR(20) NOT NULL DEFAULT 'detected' COMMENT 'detected/confirmed/rejected/queued/running/completed/failed',
    confirmed_at DATETIME COMMENT '确认时间（人工或自动）',
    queued_at DATETIME COMMENT '推入 MNS 时间（用于幂等校验）',
    error_msg TEXT,
    video_raw_data JSON COMMENT '视频数据快照',
    author_raw_data JSON COMMENT '作者数据快照',
    tag_groups JSON COMMENT 'OpenAI生成的标签',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_stat_current (stat_id_current),
    INDEX idx_strategy_id (strategy_id),
    INDEX idx_status (status) COMMENT 'Dispatcher 扫描 confirmed',
    INDEX idx_export_status (export_id, status),
    INDEX idx_created_at (created_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

---

## 9. 配置项扩展

`internal/config/config.go` 新增：

```go
// Auto Promote
AutoPromoteEnabled            bool   // 总开关，默认 false
AutoPromoteVideoRangeDays     int    // 刷新视频列表的日期范围（天），默认 30
AutoPromoteVideoCooldownSec   int    // 同视频投放冷却时间（秒），默认 60

// MNS
MNSEndpoint        string // 阿里云 MNS 接入点
MNSAccessKeyID     string // Access Key ID（可复用 OSS 的）
MNSAccessKeySecret string // Access Key Secret
MNSQueueName       string // 队列名称，默认 "auto-promote-queue"
```

---

## 10. 开放问题

| # | 问题 | 当前默认 |
|---|------|---------|
| 1 | `promotion_target` 的诸葛 API 值？ | 情况一: 推荐(9), 情况二: 关注(需确认具体值) |
| 2 | OpenAI 生成标签时的 `script` 从哪取？ | 使用视频 `description` 作为文案传入 |
| 3 | MNS 使用队列模式还是主题模式？ | 队列模式（Queue），1对1消费 |
| 4 | 失败消息重试策略？ | MNS 自带重试 + `auto_promote_logs` 状态兜底 |
