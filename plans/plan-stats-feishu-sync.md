# Plan: video_stats 飞书电子表格同步

> **状态**: 待开发
> **创建时间**: 2026-03-30
> **关联模块**: `video_stats`, `zhuge_authors`, `author_videos`

---

## 1. 需求概述

### 1.1 业务目标

1. `video_stats`（外部系统每 30 分钟写入）需关联 `author_id`
2. 定时任务每 30 分钟将 `video_stats` 数据**增量追加**到飞书电子表格
3. **每个作者 × 每个月** = 一个飞书电子表格文档
4. 每个电子表格内按 **日期 + 视频序号** 区分 sheet

### 1.2 飞书文档组织结构

```
飞书
├── 作者A - 2026年03月            ← 一个电子表格文档
│     ├── Sheet "03-30-a"         ← 3月30日 视频A 的采集记录
│     ├── Sheet "03-30-b"         ← 3月30日 视频B 的采集记录
│     ├── Sheet "03-30-c"         ← 3月30日 视频C 的采集记录
│     ├── Sheet "03-29-a"         ← 3月29日 视频A
│     └── Sheet "03-29-b"         ← 3月29日 视频B
│
├── 作者A - 2026年04月            ← 跨月自动新建电子表格
│     └── Sheet "04-01-a"
│
└── 作者B - 2026年03月
      ├── Sheet "03-30-a"
      └── Sheet "03-30-b"
```

**Sheet 命名规则**：`{MM-dd}-{序号}`
- 同一天同一作者下的多个视频，按 `export_id` 字典序排序后分配序号 `a`, `b`, `c`...
- 每个 sheet 内容为该视频在该天的所有采集记录（每 30 分钟追加一行）

### 1.3 数据流架构

```
外部系统每 30 分钟写入 video_stats
  POST /api/public/video-stats
      │
      │ (写入时) 查 author_videos 表反查 author_id
      │          → 找到: 填充 author_id
      │          → 找不到: author_id 留 NULL
      │
      ▼
┌─── Stats Cron（cmd/cron/stats, 每 30 分钟触发）──────────────┐
│                                                               │
│  Step 1: 查所有启用飞书同步的作者 (feishu_sync_enabled=true)   │
│  Step 2: 对每个作者：                                          │
│          a) 确定当月电子表格（不存在则创建）                     │
│          b) 查该作者未同步的 video_stats 增量记录               │
│          c) 按 (日期, export_id) 分组                          │
│          d) 确定目标 sheet（不存在则创建 + 写表头）              │
│          e) 追加数据行到对应 sheet                              │
│  Step 3: 更新同步游标                                          │
│                                                               │
└───────────────────────────────────────────────────────────────┘
```

---

## 2. 飞书电子表格列结构

每个 sheet 内的列定义（与现有 CSV 导出格式一致）：

| 列 | 表头 | 数据来源（`video_stats` 字段） |
|----|------|-------------------------------|
| A | 采集时间 | `created_at` |
| B | 视频描述 | `description` |
| C | 视频ID | `export_id` |
| D | 发布时间 | `publish_date` |
| E | 完播率 | `completion_rate` |
| F | 平均播放时长 | `avg_play_duration` |
| G | 播放量 | `play_count` |
| H | 推荐 | `recommend_count` |
| I | 喜欢 | `like_count` |
| J | 评论量 | `comment_count` |
| K | 分享量 | `share_count` |
| L | 关注量 | `follow_count` |
| M | 转发聊天和朋友圈 | `forward_count` |
| N | 设为铃声 | `ringtone_count` |
| O | 设为状态 | `status_count` |
| P | 设为朋友圈封面 | `cover_count` |

---

## 3. 数据库设计

### 3.1 `video_stats` 新增 `author_id` 列

```sql
ALTER TABLE video_stats
  ADD COLUMN author_id VARCHAR(128) DEFAULT NULL COMMENT '作者ID，关联 zhuge_authors.id' AFTER export_id,
  ADD INDEX idx_author_id (author_id);
```

> `author_id` 可空：外部写入时若无法匹配到作者则留 NULL。

### 3.2 `zhuge_authors` 新增飞书配置列

```sql
ALTER TABLE zhuge_authors
  ADD COLUMN feishu_sync_enabled TINYINT(1) DEFAULT 0 COMMENT '是否启用飞书同步',
  ADD COLUMN feishu_folder_token VARCHAR(128) DEFAULT NULL COMMENT '飞书文件夹 token（文档创建在此文件夹下）';
```

> 每个作者的每月电子表格由 Cron 自动创建到 `feishu_folder_token` 指定的文件夹下。

### 3.3 新增表：`feishu_sync_cursors`（增量同步游标）

```sql
CREATE TABLE IF NOT EXISTS feishu_sync_cursors (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    author_id VARCHAR(128) NOT NULL COMMENT '作者ID',
    last_synced_at DATETIME NOT NULL COMMENT '上次同步到的 video_stats.created_at',
    last_synced_stat_id CHAR(36) COMMENT '上次同步到的 video_stats.id',
    sync_count INT DEFAULT 0 COMMENT '累计同步记录数',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_author_id (author_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

### 3.4 新增表：`feishu_spreadsheets`（作者月度电子表格注册表）

记录已创建的飞书电子表格，避免每月重复创建。

```sql
CREATE TABLE IF NOT EXISTS feishu_spreadsheets (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    author_id VARCHAR(128) NOT NULL COMMENT '作者ID',
    month VARCHAR(7) NOT NULL COMMENT '月份，格式 2026-03',
    spreadsheet_token VARCHAR(128) NOT NULL COMMENT '飞书电子表格 token',
    title VARCHAR(256) NOT NULL COMMENT '电子表格标题，如 "作者A - 2026年03月"',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_author_month (author_id, month)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

---

## 4. 分层实现方案

### 4.1 Model 层

#### `internal/model/video_stat.go`（改造：新增字段）

```go
type VideoStat struct {
    // ... 现有字段
    ExportID  string  `gorm:"type:varchar(256);not null" json:"export_id"`
    AuthorID  *string `gorm:"type:varchar(128);index" json:"author_id"` // 新增
    // ...
}
```

#### `internal/model/zhuge_author.go`（改造：新增字段）

```go
type ZhugeAuthor struct {
    // ... 现有字段
    FeishuSyncEnabled  bool    `gorm:"default:false" json:"feishu_sync_enabled"`            // 新增
    FeishuFolderToken  *string `gorm:"type:varchar(128)" json:"feishu_folder_token"`         // 新增
    // ...
}
```

#### `internal/model/feishu_sync_cursor.go`（新增）

```go
type FeishuSyncCursor struct {
    ID               string    `gorm:"primaryKey;type:char(36)" json:"id"`
    AuthorID         string    `gorm:"type:varchar(128);not null;uniqueIndex" json:"author_id"`
    LastSyncedAt     time.Time `gorm:"type:datetime;not null" json:"last_synced_at"`
    LastSyncedStatID *string   `gorm:"type:char(36)" json:"last_synced_stat_id"`
    SyncCount        int       `gorm:"default:0" json:"sync_count"`
    CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
    UpdatedAt        time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (FeishuSyncCursor) TableName() string { return "feishu_sync_cursors" }
```

#### `internal/model/feishu_spreadsheet.go`（新增）

```go
type FeishuSpreadsheet struct {
    ID               string    `gorm:"primaryKey;type:char(36)" json:"id"`
    AuthorID         string    `gorm:"type:varchar(128);not null" json:"author_id"`
    Month            string    `gorm:"type:varchar(7);not null" json:"month"`              // "2026-03"
    SpreadsheetToken string    `gorm:"type:varchar(128);not null" json:"spreadsheet_token"`
    Title            string    `gorm:"type:varchar(256);not null" json:"title"`
    CreatedAt        time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (FeishuSpreadsheet) TableName() string { return "feishu_spreadsheets" }
```

---

### 4.2 Repository 层

> **风格对齐**：与现有 Repo 方法保持一致，不传 `context.Context`。

#### `internal/repository/video_stat_repo.go`（改造：新增方法）

```go
// GetUnsyncedByAuthorID 获取某作者在 lastSyncedAt 之后的 video_stats
// 按 created_at ASC 排序，确保增量追加顺序正确
func (r *VideoStatRepo) GetUnsyncedByAuthorID(authorID string, lastSyncedAt time.Time) ([]model.VideoStat, error)
```

#### `internal/repository/zhuge_author_repo.go`（改造：新增方法）

```go
// ListFeishuEnabled 获取所有启用飞书同步的作者
func (r *ZhugeAuthorRepo) ListFeishuEnabled() ([]model.ZhugeAuthor, error)

// GetByID 按主键查询单个作者
func (r *ZhugeAuthorRepo) GetByID(id string) (*model.ZhugeAuthor, error)
```

#### `internal/repository/feishu_sync_cursor_repo.go`（新增）

```go
type FeishuSyncCursorRepo struct { db *gorm.DB }

func NewFeishuSyncCursorRepo(db *gorm.DB) *FeishuSyncCursorRepo

func (r *FeishuSyncCursorRepo) GetByAuthorID(authorID string) (*model.FeishuSyncCursor, error)
func (r *FeishuSyncCursorRepo) Upsert(cursor *model.FeishuSyncCursor) error
```

#### `internal/repository/feishu_spreadsheet_repo.go`（新增）

```go
type FeishuSpreadsheetRepo struct { db *gorm.DB }

func NewFeishuSpreadsheetRepo(db *gorm.DB) *FeishuSpreadsheetRepo

func (r *FeishuSpreadsheetRepo) GetByAuthorMonth(authorID, month string) (*model.FeishuSpreadsheet, error)
func (r *FeishuSpreadsheetRepo) Create(ss *model.FeishuSpreadsheet) error
```

---

### 4.3 飞书客户端（新增）

#### `internal/pkg/feishu/client.go`

```go
type Client struct {
    appID     string
    appSecret string
    token     string    // tenant_access_token
    tokenExp  time.Time
    client    *http.Client
}

func NewClient(appID, appSecret string) *Client
```

**核心 API 方法**：

```go
// ── 认证 ──

// GetTenantAccessToken 获取/刷新 tenant_access_token
// POST https://open.feishu.cn/open-apis/auth/v3/tenant_access_token/internal/
func (c *Client) EnsureToken() error

// ── 电子表格操作 ──

// CreateSpreadsheet 在指定文件夹下创建电子表格
// POST https://open.feishu.cn/open-apis/sheets/v3/spreadsheets
func (c *Client) CreateSpreadsheet(folderToken, title string) (spreadsheetToken string, err error)

// GetSheets 获取电子表格所有 sheet 列表
// GET https://open.feishu.cn/open-apis/sheets/v3/spreadsheets/{token}/sheets/query
func (c *Client) GetSheets(spreadsheetToken string) ([]SheetInfo, error)

// CreateSheet 新增 sheet 页
// POST https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/{token}/sheets_batch_update
func (c *Client) CreateSheet(spreadsheetToken, title string) (sheetID string, err error)

// WriteHeader 写入表头行（A1:P1）
// PUT https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/{token}/values
func (c *Client) WriteHeader(spreadsheetToken, sheetID string, headers []string) error

// AppendRows 追加数据行
// POST https://open.feishu.cn/open-apis/sheets/v2/spreadsheets/{token}/values_append
func (c *Client) AppendRows(spreadsheetToken, sheetID string, rows [][]interface{}) error
```

#### `internal/pkg/feishu/types.go`

```go
type SheetInfo struct {
    SheetID string `json:"sheet_id"`
    Title   string `json:"title"`
    Index   int    `json:"index"`
}

type FeishuResponse struct {
    Code int             `json:"code"`
    Msg  string          `json:"msg"`
    Data json.RawMessage `json:"data"`
}
```

---

### 4.4 Service 层

#### `internal/service/stats_sync.go`（新增）

```go
const (
    statsSyncTimeout = 55 * time.Second
)

type StatsSyncService struct {
    authorRepo      *repository.ZhugeAuthorRepo
    videoStatRepo   *repository.VideoStatRepo
    cursorRepo      *repository.FeishuSyncCursorRepo
    spreadsheetRepo *repository.FeishuSpreadsheetRepo
    feishuClient    *feishu.Client
}
```

**核心方法**：

```go
// Run 同步主入口（Cron 每 30 分钟调一次）
// 作者级并发：最多 5 个作者同时同步，单作者失败不影响其他
func (s *StatsSyncService) Run() string {
    // 1. 查所有启用飞书同步的作者
    authors, err := s.authorRepo.ListFeishuEnabled()
    if err != nil {
        log.Printf("[stats-sync] list authors error: %v", err)
        return "error: list authors"
    }

    var (
        mu          sync.Mutex
        totalSynced int
    )

    g, _ := errgroup.WithContext(context.Background())
    g.SetLimit(5) // 控制并发度，避免飞书 API 限流

    for _, author := range authors {
        author := author // capture loop var
        g.Go(func() error {
            count, err := s.syncAuthor(author)
            if err != nil {
                log.Printf("[stats-sync] sync author=%s error: %v", author.ID, err)
                return nil // 不中断其他作者
            }
            mu.Lock()
            totalSynced += count
            mu.Unlock()
            return nil
        })
    }
    g.Wait()

    return fmt.Sprintf("synced %d records for %d authors", totalSynced, len(authors))
}

// syncAuthor 同步单个作者
func (s *StatsSyncService) syncAuthor(author model.ZhugeAuthor) (int, error) {
    // 1. 获取同步游标 → 确定增量起点
    cursor, _ := s.cursorRepo.GetByAuthorID(author.ID)
    lastSyncedAt := time.Time{}
    if cursor != nil {
        lastSyncedAt = cursor.LastSyncedAt
    }

    // 2. 查询未同步的 video_stats（按 created_at ASC）
    stats, err := s.videoStatRepo.GetUnsyncedByAuthorID(author.ID, lastSyncedAt)
    if err != nil { return 0, err }
    if len(stats) == 0 { return 0, nil }

    // 3. 按 (月份, 日期, export_id) 分组
    monthGroups := groupByMonth(stats) // map["2026-03"][]VideoStat

    for month, monthStats := range monthGroups {
        // 4. 确定当月电子表格（不存在 → 创建）
        spreadsheetToken, err := s.ensureSpreadsheet(author, month)
        if err != nil { return 0, err }

        // 5. 获取已有 sheet 列表
        sheets, _ := s.feishuClient.GetSheets(spreadsheetToken)

        // 6. 按 (日期, export_id) 分组 → 确定 sheet 名
        dateVideoGroups := groupByDateAndVideo(monthStats)
        // 结果: map["03-30"]["export_id_xxx"][]VideoStat

        for date, videoMap := range dateVideoGroups {
            // 按 export_id 字典序排列 → 分配序号 a/b/c...
            sortedExportIDs := sortKeys(videoMap)

            for idx, exportID := range sortedExportIDs {
                sheetTitle := fmt.Sprintf("%s-%s", date, string(rune('a'+idx)))
                // 例: "03-30-a", "03-30-b", "03-30-c"

                sheetID := findSheetByTitle(sheets, sheetTitle)

                // 7. sheet 不存在 → 创建 + 写表头
                if sheetID == "" {
                    sheetID, _ = s.feishuClient.CreateSheet(spreadsheetToken, sheetTitle)
                    s.feishuClient.WriteHeader(spreadsheetToken, sheetID, feishuHeaders)
                }

                // 8. 追加数据行
                rows := buildRows(videoMap[exportID])
                s.feishuClient.AppendRows(spreadsheetToken, sheetID, rows)
            }
        }
    }

    // 9. 更新游标
    lastStat := stats[len(stats)-1]
    s.cursorRepo.Upsert(&model.FeishuSyncCursor{...})

    return len(stats), nil
}

// ensureSpreadsheet 确保当月电子表格存在，不存在则创建
func (s *StatsSyncService) ensureSpreadsheet(author model.ZhugeAuthor, month string) (string, error) {
    // 查 feishu_spreadsheets 表
    existing, _ := s.spreadsheetRepo.GetByAuthorMonth(author.ID, month)
    if existing != nil {
        return existing.SpreadsheetToken, nil
    }

    // 创建新电子表格
    // 标题格式: "{作者昵称} - {年}年{月}月"
    // 例: "大宝见 666 - 2026年03月"
    title := fmt.Sprintf("%s - %s", author.Nickname, formatMonthTitle(month))
    token, err := s.feishuClient.CreateSpreadsheet(*author.FeishuFolderToken, title)
    if err != nil { return "", err }

    // 写入 DB 记录
    s.spreadsheetRepo.Create(&model.FeishuSpreadsheet{
        AuthorID:         author.ID,
        Month:            month,
        SpreadsheetToken: token,
        Title:            title,
    })

    return token, nil
}
```

**辅助函数**：

```go
// feishuHeaders 飞书表头（对应列 A-P）
var feishuHeaders = []string{
    "采集时间", "视频描述", "视频ID", "发布时间",
    "完播率", "平均播放时长", "播放量", "推荐",
    "喜欢", "评论量", "分享量", "关注量",
    "转发聊天和朋友圈", "设为铃声", "设为状态", "设为朋友圈封面",
}

// buildRows 将 video_stats 转为飞书行数据
func buildRows(stats []model.VideoStat) [][]interface{} {
    rows := make([][]interface{}, len(stats))
    for i, s := range stats {
        rows[i] = []interface{}{
            s.CreatedAt,        // A
            s.Description,      // B
            s.ExportID,         // C
            s.PublishDate,      // D
            s.CompletionRate,   // E
            s.AvgPlayDuration,  // F
            s.PlayCount,        // G
            s.RecommendCount,   // H
            s.LikeCount,        // I
            s.CommentCount,     // J
            s.ShareCount,       // K
            s.FollowCount,      // L
            s.ForwardCount,     // M
            s.RingtoneCount,    // N
            s.StatusCount,      // O
            s.CoverCount,       // P
        }
    }
    return rows
}

// groupByMonth 按月份分组
func groupByMonth(stats []model.VideoStat) map[string][]model.VideoStat {
    // key: "2026-03"
    grouped := make(map[string][]model.VideoStat)
    for _, s := range stats {
        month := s.CreatedAt[:7] // "2026-03"
        grouped[month] = append(grouped[month], s)
    }
    return grouped
}

// groupByDateAndVideo 按 (日期, export_id) 二级分组
func groupByDateAndVideo(stats []model.VideoStat) map[string]map[string][]model.VideoStat {
    // 第一层 key: "03-30"，第二层 key: export_id
    grouped := make(map[string]map[string][]model.VideoStat)
    for _, s := range stats {
        date := s.CreatedAt[5:10] // "03-30"
        if grouped[date] == nil {
            grouped[date] = make(map[string][]model.VideoStat)
        }
        grouped[date][s.ExportID] = append(grouped[date][s.ExportID], s)
    }
    return grouped
}

// formatMonthTitle 将 "2026-03" 格式化为 "2026年03月"
func formatMonthTitle(month string) string {
    parts := strings.Split(month, "-")
    return fmt.Sprintf("%s年%s月", parts[0], parts[1])
}
```

---

### 4.5 `video_stats` 写入时关联 `author_id`

#### 改造 `internal/service/video_stat.go`（Service 层）

在 `BulkUpsert` 之前由 Service 层调用填充，Handler 保持 thin wrapper：

```go
// FillAuthorIDs 通过 author_videos 表反查 export_id → author_id
func (s *VideoStatService) FillAuthorIDs(stats []model.VideoStat) {
    for i := range stats {
        if stats[i].AuthorID != nil {
            continue
        }
        av, _ := s.authorVideoRepo.GetByExportID(stats[i].ExportID)
        if av != nil {
            stats[i].AuthorID = &av.AuthorID
        }
        // 找不到 → 留 nil，不阻塞写入
    }
}
```

> **依赖**：`author_videos` 表（`plan-auto-promote` Phase 1 中实现）。
> 当前若 `author_videos` 中无数据，`author_id` 留空，后续可补偿回填。

---

### 4.6 入口

#### `cmd/cron/stats/main.go`（新增）

```go
package main

import (
    "context"
    "fmt"
    "log"
    "os"
    "time"

    "github.com/AiMarketool/f2v-promote/internal/config"
    "github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
    "github.com/AiMarketool/f2v-promote/internal/repository"
    "github.com/AiMarketool/f2v-promote/internal/service"
    "github.com/aliyun/fc-runtime-go-sdk/events"
    fc "github.com/aliyun/fc-runtime-go-sdk/fc"
    "gorm.io/driver/mysql"
    "gorm.io/gorm"
)

var syncSvc *service.StatsSyncService

func initService() {
    cfg := config.Load()

    db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{})
    if err != nil {
        log.Fatalf("failed to connect to MySQL: %v", err)
    }

    sqlDB, _ := db.DB()
    sqlDB.SetMaxIdleConns(5)
    sqlDB.SetMaxOpenConns(20)
    sqlDB.SetConnMaxLifetime(time.Hour)

    feishuClient := feishu.NewClient(cfg.FeishuAppID, cfg.FeishuAppSecret)

    syncSvc = service.NewStatsSyncService(
        repository.NewZhugeAuthorRepo(db),
        repository.NewVideoStatRepo(db),
        repository.NewFeishuSyncCursorRepo(db),
        repository.NewFeishuSpreadsheetRepo(db),
        feishuClient,
    )
}

// initialize FC 初始化钩子（实例启动时调用一次）
func initialize(ctx context.Context) {
    initService()
}

// preStop FC 预停止钩子（实例销毁前调用）
func preStop(ctx context.Context) {
}

// HandleRequest FC 定时触发器入口
func HandleRequest(ctx context.Context, event events.Data) (string, error) {
    if syncSvc == nil {
        initService()
    }
    return syncSvc.Run(), nil
}

func main() {
    if os.Getenv("FC_RUNTIME_API") == "" {
        fmt.Println("本地运行模式，开始同步 video_stats 到飞书...")
        initService()
        for {
            result := syncSvc.Run()
            fmt.Println(result)
            time.Sleep(30 * time.Minute)
        }
    } else {
        fc.RegisterInitializerFunction(initialize)
        fc.RegisterPreStopFunction(preStop)
        fc.Start(HandleRequest)
    }
}
```

---

## 5. 配置项扩展

`internal/config/config.go` 新增：

```go
// Feishu
FeishuAppID     string // 飞书应用 App ID
FeishuAppSecret string // 飞书应用 App Secret
```

`.env` 新增：

```
FEISHU_APP_ID=cli_xxxxx
FEISHU_APP_SECRET=xxxxx
```

---

## 6. 部署配置

#### `s.cron-stats.prod.yaml`（新增）

```yaml
edition: 3.0.0
name: f2v-promote-cron-stats
resources:
  f2v-promote-cron-stats:
    component: fc3
    props:
      region: cn-hangzhou
      functionName: f2v-promote-cron-stats
      runtime: custom
      handler: index.handler
      timeout: 120
      memorySize: 256
      triggers:
        - triggerName: timer
          triggerType: timer
          triggerConfig:
            cronExpression: "0 */30 * * * *"
            enable: true
```

---

## 7. 飞书 API 权限

需在飞书开放平台应用中开启：

| 权限 | 用途 |
|------|------|
| `sheets:spreadsheet` | 创建/读写电子表格 |
| `drive:drive` | 在文件夹下创建文档 |

需将飞书应用添加为目标文件夹的协作者（编辑权限）。

---

## 8. 开发任务清单

### Phase 1: 数据层（Model + Repository + 迁移）

| # | 任务 | 文件 |
|---|------|------|
| 1.1 | `video_stats` 新增 `author_id` 字段 | `internal/model/video_stat.go` |
| 1.2 | `zhuge_authors` 新增飞书配置字段 | `internal/model/zhuge_author.go` |
| 1.3 | 新增 `FeishuSyncCursor` Model | `internal/model/feishu_sync_cursor.go` |
| 1.4 | 新增 `FeishuSpreadsheet` Model | `internal/model/feishu_spreadsheet.go` |
| 1.5 | SQL 迁移脚本 | `mysql/migration_stats_feishu.sql` |

### Phase 2: Repository 层

| # | 任务 | 文件 |
|---|------|------|
| 2.1 | `VideoStatRepo` 新增增量查询方法 | `internal/repository/video_stat_repo.go` |
| 2.2 | `ZhugeAuthorRepo` 新增飞书相关查询 | `internal/repository/zhuge_author_repo.go` |
| 2.3 | 新增 `FeishuSyncCursorRepo` | `internal/repository/feishu_sync_cursor_repo.go` |
| 2.4 | 新增 `FeishuSpreadsheetRepo` | `internal/repository/feishu_spreadsheet_repo.go` |

### Phase 3: 飞书客户端

| # | 任务 | 文件 |
|---|------|------|
| 3.1 | 飞书认证 + 电子表格 API 封装 | `internal/pkg/feishu/client.go` |
| 3.2 | 响应/配置结构体 | `internal/pkg/feishu/types.go` |

### Phase 4: Service + Handler 改造

| # | 任务 | 文件 |
|---|------|------|
| 4.1 | `StatsSyncService` 同步核心逻辑 | `internal/service/stats_sync.go` |
| 4.2 | `VideoStatHandler` 写入时填充 `author_id` | `internal/handler/v1/video_stat.go` |

### Phase 5: 入口 + 部署 + 配置

| # | 任务 | 文件 |
|---|------|------|
| 5.1 | Stats Cron 入口 | `cmd/cron/stats/main.go` |
| 5.2 | Config 新增飞书配置 | `internal/config/config.go` |
| 5.3 | 部署配置 | `s.cron-stats.prod.yaml` |

---

## 9. SQL 迁移脚本汇总

```sql
-- mysql/migration_stats_feishu.sql

-- 1. video_stats 新增 author_id
ALTER TABLE video_stats
  ADD COLUMN author_id VARCHAR(128) DEFAULT NULL
    COMMENT '作者ID，关联 zhuge_authors.id' AFTER export_id,
  ADD INDEX idx_author_id (author_id);

-- 2. zhuge_authors 新增飞书配置
ALTER TABLE zhuge_authors
  ADD COLUMN feishu_sync_enabled TINYINT(1) DEFAULT 0
    COMMENT '是否启用飞书同步',
  ADD COLUMN feishu_folder_token VARCHAR(128) DEFAULT NULL
    COMMENT '飞书文件夹 token';

-- 3. 增量同步游标表
CREATE TABLE IF NOT EXISTS feishu_sync_cursors (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    author_id VARCHAR(128) NOT NULL COMMENT '作者ID',
    last_synced_at DATETIME NOT NULL COMMENT '上次同步的 created_at',
    last_synced_stat_id CHAR(36) COMMENT '上次同步的 video_stats.id',
    sync_count INT DEFAULT 0 COMMENT '累计同步记录数',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    UNIQUE KEY uk_author_id (author_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

-- 4. 作者月度电子表格注册表
CREATE TABLE IF NOT EXISTS feishu_spreadsheets (
    id CHAR(36) PRIMARY KEY DEFAULT (UUID()),
    author_id VARCHAR(128) NOT NULL COMMENT '作者ID',
    month VARCHAR(7) NOT NULL COMMENT '月份 2026-03',
    spreadsheet_token VARCHAR(128) NOT NULL COMMENT '飞书电子表格 token',
    title VARCHAR(256) NOT NULL COMMENT '电子表格标题',
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE KEY uk_author_month (author_id, month)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
```

---

## 10. 开放问题

| # | 问题 | 当前默认 |
|---|------|---------|
| 1 | `author_videos` 未实现时 `author_id` 如何回填？ | 先手动 SQL 回填，或后续实现 `author_videos` 后跑补偿脚本 |
| 2 | 同一视频跨天的 sheet 序号？ | 每天独立编号，`a` 从当天第一个视频开始 |
| 3 | 飞书 API 限流？ | 单应用 QPS ≈ 100，每 30 分钟同步不会触发 |
| 4 | 电子表格默认 sheet（Sheet1）？ | 创建后删除或重命名为说明页 |
| 5 | 历史数据是否需要同步？ | 首次启动可配置全量同步（游标为零值） |
