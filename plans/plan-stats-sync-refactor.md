# 飞书数据同步维度重构计划

## 1. 背景与现状

### 当前文件夹/表格层级

```text
作者（文件夹）
└── 年月（文件夹，如 2026年04月）
    └── 平台（文件夹，如 微信）
        └── 日期.xlsx（每天一个表格，如 04-03）
            ├── Sheet a（视频1 的所有采集记录）
            ├── Sheet b（视频2）
            └── Sheet c（视频3）
```

**问题**：

- 一天可能采集十几个视频的数据，产生十几个 sheet 页，但它们分散在**按采集日期创建的表格**里
- 同一个视频的数据跨天后会出现在不同的表格中，不便追踪
- Sheet 页命名为 `a, b, c` 无实际含义，无法一眼识别是哪个视频
- 缺少"发布后多少小时"参考值，无法直观评估数据衰减曲线
- 缺少关键效率指标（单小时播放量、推荐率、评论率、关注率、转发率）

### 当前表头

```text
采集时间 | 视频描述 | 视频ID | 发布时间 | 完播率 | 平均播放时长 |
播放量 | 推荐 | 喜欢 | 评论量 | 分享量 | 关注量 |
转发聊天和朋友圈 | 设为铃声 | 设为状态 | 设为朋友圈封面
```

---

## 2. 目标设计

### 新文件夹/表格层级

```text
作者（文件夹）
└── 年月（文件夹，如 2026年04月）
    └── 平台（文件夹，如 微信）
        └── 月度表格.xlsx（每月一个表格，如 2026年04月）
            ├── Sheet "04-01"（4月1日发布的视频）
            ├── Sheet "04-03"（4月3日发布的视频）
            └── Sheet "04-05"（4月5日发布的视频）
```

**核心变化**：

| 维度 | 旧方案 | 新方案 |
| --- | --- | --- |
| 表格粒度 | 每天一个表格（按采集日期） | **每月一个表格** |
| Sheet 命名 | `a, b, c...`（无意义） | **视频发布日期**（`04-03`） |
| Sheet 分组依据 | `export_id`（一个视频一个 sheet） | **发布日期**（同一天发布的多个视频合并到一个 sheet） |
| 数据列 | 16 列基础指标 | **新增 6 列**效率指标 |

### 新表头（22 列）

```text
采集时间 | 发布后小时 | 视频描述 | 视频ID | 发布时间 | 完播率 | 平均播放时长 |
播放量 | 推荐 | 喜欢 | 评论量 | 分享量 | 关注量 |
转发聊天和朋友圈 | 设为铃声 | 设为状态 | 设为朋友圈封面 |
单小时播放量 | 推荐率 | 评论率 | 关注率 | 转发率
```

**新增列说明**（基于图片中的公式）：

| 列名 | 计算公式 |
| --- | --- |
| `发布后小时` | `采集时间 - 发布时间`（小时数，浮点数，如 `24.5`） |
| `单小时播放量` | `(本次播放量 - 上一次播放量) ÷ (本次采集时间 - 上一次采集时间的小时差)` |
| `推荐率` | `(本次推荐 - 上一次推荐) ÷ (本次播放量 - 上一次播放量)` |
| `评论率` | `(本次评论量 - 上一次评论量) ÷ (本次播放量 - 上一次播放量)` |
| `关注率` | `(本次关注量 - 上一次关注量) ÷ (本次播放量 - 上一次播放量)` |
| `转发率` | `(本次分享量 - 上一次分享量) ÷ (本次播放量 - 上一次播放量)` |

> **注意**：效率指标需要"上一次记录"，每个视频的第一条记录这些列为空。增量播放为 0 时分母为 0，填 `"-"`。

---

## 3. 数据模型变更

### 3.1 `author_videos` 新增 `create_time` 字段

**目的**：存储第三方平台原始的视频创建时间（精确发布时间），替代从 `video_stats.publish_date` 获取的手动抓取值。

```go
type AuthorVideo struct {
    // ... 已有字段
    CreateTime *time.Time `gorm:"type:datetime" json:"create_time"` // 第三方平台原始发布时间
}
```

**数据来源**：从平台 API 的 `create_time` 字段解析（Unix 时间戳 → `time.Time`）。

### 3.2 `feishu_spreadsheets` 维度变更

**旧**：`author_id + date + platform`（每天一个表格）

**新**：`author_id + month + platform`（每月一个表格）

```go
type FeishuSpreadsheet struct {
    Base
    AuthorID         int64  `gorm:"not null;index" json:"author_id"`
    Platform         string `gorm:"type:varchar(32);default:'weixin'" json:"platform"`
    Month            string `gorm:"type:varchar(7)" json:"month"`     // "2026-04"（旧 Date 字段改为 Month）
    SpreadsheetToken string `gorm:"type:varchar(128);not null" json:"spreadsheet_token"`
    Title            string `gorm:"type:varchar(256);not null" json:"title"`
}
```

### 3.3 `feishu_sheet_tabs` 维度变更

**旧**：`export_id`（每个视频一个 sheet tab）

**新**：`publish_date`（每个发布日期一个 sheet tab，一个 sheet 内混合多个视频）

```go
type FeishuSheetTab struct {
    Base
    AuthorID         int64  `gorm:"not null;index:idx_sheet_tab_lookup" json:"author_id"`
    Month            string `gorm:"type:varchar(7);not null;index:idx_sheet_tab_lookup" json:"month"`         // "2026-04"
    PublishDate      string `gorm:"type:varchar(10);not null;index:idx_sheet_tab_lookup" json:"publish_date"` // "2026-04-03"
    SpreadsheetToken string `gorm:"type:varchar(128);not null" json:"spreadsheet_token"`
    SheetID          string `gorm:"type:varchar(64);not null" json:"sheet_id"`
    SheetTitle       string `gorm:"type:varchar(32);not null" json:"sheet_title"` // "04-03"（发布日期简写）
}
```

---

## 4. 同步流程重构

### 现有流程（按采集日期）

```text
遍历作者 → 按 collect_date 分组 → 每天创建一个表格 → 按 export_id 创建 sheet → 追加行
```

### 新流程（按发布日期）

```text
遍历作者
  → 获取待同步 video_stats
  → 关联 author_videos 获取每个视频的 create_time（精确发布时间）
  → 按 month + platform 确保月度表格
  → 按 publish_date 确保 sheet tab
  → 构建数据行（含效率指标 + 发布后小时数）
  → 追加行到对应 sheet
```

### 关键变更

| 步骤 | 旧 | 新 |
| --- | --- | --- |
| 分组键 | `collect_date`（采集日期） | `publish_date`（发布日期，来自 `author_videos.create_time`） |
| 表格粒度 | `ensureDaySpreadsheet(date)` | `ensureMonthSpreadsheet(month)` |
| Sheet 查找 | `tabMap[exportID]` | `tabMap[publishDate]` |
| Sheet 命名 | `a, b, c...` | `"04-03"`（发布日的 `MM-DD`） |
| 数据行 | 16 列原始指标 | 22 列（+发布后小时 +5个效率指标） |

---

## 5. 效率指标计算逻辑

### 计算前置条件

对于每个视频（`export_id`），需要获取**按采集时间排序的前一条记录**来计算增量。

```go
// 方案：在构建行时，查询同 export_id 的上一条记录
prevStat, _ := videoStatRepo.GetPreviousStat(stat.ExportID, stat.ID)
```

### 计算公式（Go 伪代码）

```go
func calcEfficiencyMetrics(curr, prev *model.VideoStat, publishTime time.Time) map[string]interface{} {
    m := map[string]interface{}{}

    // 发布后小时数
    collectTime, _ := time.Parse("2006-01-02 15:04:05", curr.CollectDate)
    m["hours_since_publish"] = collectTime.Sub(publishTime).Hours()

    if prev == nil {
        return m // 第一条记录，效率指标为空
    }

    deltaPlay := curr.PlayCount - prev.PlayCount
    if deltaPlay <= 0 {
        m["hourly_plays"] = "-"
        m["recommend_rate"] = "-"
        m["comment_rate"] = "-"
        m["follow_rate"] = "-"
        m["share_rate"] = "-"
        return m
    }

    // 采集时间差（小时）
    prevTime, _ := time.Parse("2006-01-02 15:04:05", prev.CollectDate)
    deltaHours := collectTime.Sub(prevTime).Hours()

    m["hourly_plays"] = float64(deltaPlay) / deltaHours
    m["recommend_rate"] = float64(curr.RecommendCount - prev.RecommendCount) / float64(deltaPlay)
    m["comment_rate"] = float64(curr.CommentCount - prev.CommentCount) / float64(deltaPlay)
    m["follow_rate"] = float64(curr.FollowCount - prev.FollowCount) / float64(deltaPlay)
    m["share_rate"] = float64(curr.ShareCount - prev.ShareCount) / float64(deltaPlay)
    return m
}
```

---

## 6. 代码改动清单

| # | 文件 | 操作 | 描述 |
| --- | --- | --- | --- |
| 1 | `internal/model/author_video.go` | 新增字段 | `CreateTime *time.Time` |
| 2 | `internal/model/feishu_spreadsheet.go` | 改字段 | `Date` → `Month`（varchar(7)） |
| 3 | `internal/model/feishu_sheet_tab.go` | 重构字段 | `CollectDate+ExportID` → `Month+PublishDate` |
| 4 | `internal/repository/feishu_spreadsheet_repo.go` | 改方法 | `GetByAuthorDatePlatform` → `GetByAuthorMonthPlatform` |
| 5 | `internal/repository/feishu_sheet_tab_repo.go` | 改方法 | `ListByAuthorDate` → `ListByAuthorMonthPublishDate` |
| 6 | `internal/repository/video_stat_repo.go` | 新增方法 | `GetPreviousStat(exportID string, currentID int64)` |
| 7 | `internal/repository/author_video_repo.go` | 新增方法 | `GetByExportIDs(exportIDs []string)` 批量查询 |
| 8 | `internal/repository/strategy_repo.go` | 新增方法 | `GetByAuthorID(authorID int64)` 查询作者阈值 |
| 9 | `internal/pkg/feishu/client.go` | 新增方法 | `BatchSetCellStyle()` 批量设置单元格背景色 |
| 10 | `internal/service/stats_sync.go` | 重构 | 同步流程按发布日期分组、月度表格、效率指标计算、**阈值着色** |
| 11 | `internal/service/author_video_match.go` | 更新 | 解析平台 API 的 `create_time` 并存入 `author_videos` |

---

## 7. 阈值条件着色

### 7.1 功能描述

写入效率指标行后，比对该作者的 `author_promote_strategies` 阈值配置：

- **达标** → 单元格背景色设为 **橘色** `#FF9900`（表示可投放）
- **未达标** → 单元格背景色设为 **绿色** `#00CC66`（表示正常）

### 7.2 匹配关系

| 效率指标列 | 对应 Strategy 阈值字段 | 条件 |
| --- | --- | --- |
| 单小时播放量 (R列) | `hourly_play_threshold` | `值 >= 阈值` → 橘色 |
| 推荐率 (S列) | `like_rate_threshold` | `值 >= 阈值` → 橘色 |
| 评论率 (T列) | `comment_rate_threshold` | `值 >= 阈值` → 橘色 |
| 关注率 (U列) | `follow_rate_threshold` | `值 >= 阈值` → 橘色 |
| 转发率 (V列) | `share_rate_threshold` | `值 >= 阈值` → 橘色 |

> 无阈值配置（`strategy == nil`）或阈值为 0 的作者跳过着色，保持默认。

### 7.3 飞书 API - 批量设置单元格样式

使用飞书 [批量设置单元格样式](https://open.feishu.cn/document/server-docs/docs/sheets-v3/data-operation/batch-set-cell-style) API：

```text
PUT /sheets/v2/spreadsheets/{spreadsheetToken}/styles_batch_update
```

请求体示例：

```json
{
  "data": [
    {
      "ranges": "sheetID!R2:R2",
      "style": {
        "backColor": "#FF9900"
      }
    },
    {
      "ranges": "sheetID!S2:S2",
      "style": {
        "backColor": "#00CC66"
      }
    }
  ]
}
```

### 7.4 新增 `feishu.Client` 方法

```go
// CellStyleItem 单元格样式项
type CellStyleItem struct {
    Range string                 `json:"ranges"`
    Style map[string]interface{} `json:"style"`
}

// BatchSetCellStyle 批量设置单元格背景色
func (c *Client) BatchSetCellStyle(spreadsheetToken string, items []CellStyleItem) error {
    url := fmt.Sprintf("%s/sheets/v2/spreadsheets/%s/styles_batch_update", baseURL, spreadsheetToken)
    body := map[string]interface{}{
        "data": items,
    }
    _, err := c.doRequest("PUT", url, body)
    return err
}
```

### 7.5 着色流程

```text
追加数据行成功后
  → 查询作者的 strategy 阈值（syncAuthor 入口处预加载一次，避免重复查询）
  → 获取 sheet 当前行数（通过飞书 API 或本地计数）确定新追加行的起始行号
  → 遍历每个效率指标列，比对阈值
  → 收集需着色的 CellStyleItem 列表
  → 批量调用 BatchSetCellStyle
```

**行号获取方案**：飞书 `AppendRows` 返回的 `updatedRange` 包含实际写入范围（如 `sheetID!A5:V8`），解析起始行号即可。

伪代码：

```go
func (s *StatsSyncService) colorizeRows(
    spreadsheetToken, sheetID string,
    startRow int,    // 从 AppendRows 返回值解析得到
    metrics []EfficiencyMetrics,
    strategy *model.AuthorPromoteStrategy,
) {
    if strategy == nil { return }

    var items []feishu.CellStyleItem
    orange := map[string]interface{}{"backColor": "#FF9900"}
    green  := map[string]interface{}{"backColor": "#00CC66"}

    // 效率指标列位置（基于 22 列表头）
    colMap := map[string]string{
        "hourly_plays":   "R",
        "recommend_rate": "S",
        "comment_rate":   "T",
        "follow_rate":    "U",
        "share_rate":     "V",
    }

    thresholds := map[string]float64{
        "hourly_plays":   float64(strategy.HourlyPlayThreshold),
        "recommend_rate": strategy.LikeRateThreshold,
        "comment_rate":   strategy.CommentRateThreshold,
        "follow_rate":    strategy.FollowRateThreshold,
        "share_rate":     strategy.ShareRateThreshold,
    }

    for i, m := range metrics {
        row := startRow + i
        for key, col := range colMap {
            val := m.Get(key)
            threshold := thresholds[key]
            if val == nil || threshold <= 0 { continue }

            color := green
            if *val >= threshold { color = orange }
            items = append(items, feishu.CellStyleItem{
                Range: fmt.Sprintf("%s!%s%d:%s%d", sheetID, col, row, col, row),
                Style: color,
            })
        }
    }

    if len(items) > 0 {
        s.feishuClient.BatchSetCellStyle(spreadsheetToken, items)
    }
}
```

---

## 8. 迁移策略

### 数据库迁移

```sql
-- 1. feishu_spreadsheets 字段重命名
ALTER TABLE feishu_spreadsheets CHANGE COLUMN date month VARCHAR(7);

-- 2. feishu_sheet_tabs 字段变更（保留 export_id，新增 month + publish_date，移除 collect_date）
ALTER TABLE feishu_sheet_tabs DROP INDEX idx_sheet_tab_lookup;
ALTER TABLE feishu_sheet_tabs ADD COLUMN month VARCHAR(7) NOT NULL DEFAULT '' AFTER author_id;
ALTER TABLE feishu_sheet_tabs ADD COLUMN publish_date VARCHAR(10) NOT NULL DEFAULT '' AFTER month;
ALTER TABLE feishu_sheet_tabs DROP COLUMN collect_date;
ALTER TABLE feishu_sheet_tabs ADD INDEX idx_sheet_tab_lookup (author_id, month, export_id);
```

### 飞书历史数据

- 旧表格（按天粒度）保留不动，不做迁移
- 新同步从迁移后开始，自动创建月度表格
- 建议在月初上线，自然过渡

---

## 9. 执行顺序

1. **数据库迁移**：执行 SQL 更新表结构
2. **Model 更新**：`author_video.go` + `feishu_spreadsheet.go` + `feishu_sheet_tab.go`
3. **Repo 更新**：适配新字段、新增 `GetPreviousStat` + `GetByExportIDs` + `StrategyRepo.GetByAuthorID`
4. **飞书客户端**：`feishu/client.go` 新增 `BatchSetCellStyle` + `AppendRows` 返回写入范围
5. **解析 create_time**：`author_video_match.go` 解析平台 `create_time` 存入 DB
6. **同步重构**：`stats_sync.go` 全流程改造（分组、表头、效率指标、着色）
7. **依赖注入**：`cmd/cron/main.go` 中 `NewStatsSyncService` 注入 `StrategyRepo`
8. **测试验证**：使用真实数据验证飞书表格结构、效率指标计算和着色效果
