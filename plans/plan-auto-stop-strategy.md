# 自动关停策略 — Plan

> **状态**: Approved
> **创建时间**: 2026-04-01
> **服务形态**: 独立 Cron（`AutoStopService`）

---

## 1. 业务规则

### 规则一：互动率增量下降关停（所有订单类型）

| 项目 | 说明 |
| --- | --- |
| **监控指标** | 转发增量率、点赞增量率 |
| **数据源** | `video_stats` 表（最新两条） + `auto_promote_logs.stat_raw_data`（投放前两条） |
| **系数** | `author_promote_strategies.stop_coefficient`（默认 0.7） |
| **触发** | 任一指标的增量率 ≤ 基线增量率 × 系数 即关停 |

**两对数据**：

| 数据对 | 来源 | 说明 |
| --- | --- | --- |
| 基线对 `[S0, S1]` | `auto_promote_logs.stat_raw_data` | 检测投放时保存的前后两条 video_stat |
| 当前对 `[Sn-1, Sn]` | `video_stats` 表最新两条 | 投放后实时数据 |

**增量率计算**：

```text
基线增量点赞率 = (S1.like_count - S0.like_count) / (S1.play_count - S0.play_count)
基线增量转发率 = (S1.share_count - S0.share_count) / (S1.play_count - S0.play_count)

当前增量点赞率 = (Sn.like_count - Sn-1.like_count) / (Sn.play_count - Sn-1.play_count)
当前增量转发率 = (Sn.share_count - Sn-1.share_count) / (Sn.play_count - Sn-1.play_count)

K = strategy.stop_coefficient (默认 0.7)

关停条件（任一满足）：
  当前增量点赞率 / 基线增量点赞率 ≤ K
  OR
  当前增量转发率 / 基线增量转发率 ≤ K
```

### 规则二：吸粉成本关停（仅 `promote_type=followers`）

| 项目 | 说明 |
| --- | --- |
| **数据源** | 诸葛 `GetPlanDetail` API（订单维度的花费与涨粉） |
| **花费 C** | `PlanDetail.cost`（微信豆）÷ 10 = 元（1 元 = 10 微信豆） |
| **涨粉 F** | `PlanDetail.focus_num`（订单详情页显示的该订单带来的粉丝数） |
| **阈值** | C / F > 0.8 元/粉丝 |

```text
C = PlanDetail.cost / 10   (微信豆 → 元)
F = PlanDetail.focus_num

关停条件：F > 0  AND  C / F > 0.8
```

### 保护机制

- 投放时长 < 30 分钟 → 不评估
- `video_stats` 记录不足两条 → 跳过规则一
- 增量播放量 ≤ 0（`S1.play - S0.play ≤ 0` 或 `Sn.play - Sn-1.play ≤ 0`）→ 跳过（避免除零）
- 基线增量率 ≤ 0 → 跳过对应指标
- `focus_num` = 0 → 不触发规则二

---

## 2. 数据来源

| 数据 | 来源 | 说明 |
| --- | --- | --- |
| 基线对 [S0, S1] | `auto_promote_logs.stat_raw_data` | JSON 数组，检测时保存的前后两条 video_stat |
| 当前对 [Sn-1, Sn] | `video_stats` 表 | 通过 `author_video_id` → `export_id` 关联查最新两条 |
| 关停系数 K | `author_promote_strategies.stop_coefficient` | 通过 `auto_promote_logs.strategy_id` 关联 |
| 订单花费/涨粉 | 诸葛 `GetPlanDetail` API | `cost`、`focus_num`（仅规则二） |
| 关联订单 | `auto_promote_logs.order_id` → `orders` | `platform_order_id`, `account_id`, `status` |

---

## 3. 数据库变更

### `author_promote_strategies` 新增字段

```sql
ALTER TABLE author_promote_strategies
  ADD COLUMN stop_coefficient DECIMAL(6,3) NOT NULL DEFAULT 0.700;
```

Model 对应：

```go
// internal/model/author_promote_strategy.go 新增
StopCoefficient float64 `gorm:"type:decimal(6,3);not null;default:0.7" json:"stop_coefficient"`
```

---

## 4. 诸葛 ClosePlan API（已抓包确认）

| 项目 | 说明 |
| --- | --- |
| 端点 | `POST /api/v1/ts-plan-info/close-plan/{promotionID}` |
| Base URL | `cfg.ZhugeLoginURL`（与 `GetPlanDetail` 同源） |
| Body | 空（`content-length: 0`） |
| 鉴权 | 标准 `token` Header（现有 `Request()` 自动处理） |

---

## 5. 服务架构

```text
┌─────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐
│ Promote Cron│  │ Order Cron   │  │ Stats Cron   │  │ AutoStop Cron│
│ (检测投放)  │  │ (轮询订单)   │  │ (同步数据)   │  │ (关停评估)   │
└─────────────┘  └──────────────┘  └──────────────┘  └──────────────┘
                                        │                     │
                                        └ 写入 video_stats ──┘ 读取做规则一对比
```

### 轮询目标

```sql
SELECT apl.* FROM auto_promote_logs apl
JOIN orders o ON o.id = apl.order_id
WHERE apl.status = 'completed'
  AND apl.order_id IS NOT NULL
  AND o.status = 'active'
  AND o.platform_order_id IS NOT NULL AND o.platform_order_id != ''
```

---

## 6. 变更文件清单

### 新增文件

| 文件 | 说明 |
| --- | --- |
| `cmd/cron/auto-stop/main.go` | Cron 入口 |
| `internal/service/auto_stop.go` | `AutoStopService` 核心逻辑 |

### 修改文件

| 文件 | 变更 |
| --- | --- |
| `internal/model/author_promote_strategy.go` | 新增 `StopCoefficient` 字段 |
| `internal/center/weixin/client.go` | 新增 `ClosePlan()` 方法 |
| `internal/repository/auto_promote_log_repo.go` | 新增 `ListForStopEvaluation()` 查询 |

### 复用现有

| 文件 | 方法 | 用途 |
| --- | --- | --- |
| `internal/repository/video_stat_repo.go` | `GetLatestTwoByExportID(exportID)` | 获取最新两条 video_stat |
| `internal/repository/author_video_repo.go` | `GetVideoInfoMap(ids)` | author_video_id → export_id |
| `internal/repository/strategy_repo.go` | `GetByID(id)` / 待确认 | 查策略获取 stop_coefficient |

---

## 7. 实现步骤

### Step 1: DB Migration + Model

```go
// internal/model/author_promote_strategy.go 新增字段
StopCoefficient float64 `gorm:"type:decimal(6,3);not null;default:0.7" json:"stop_coefficient"`
```

### Step 2: `weixin.Client.ClosePlan()`

```go
// internal/center/weixin/client.go 新增（已完成）
func (c *Client) ClosePlan(accountID, promotionID string) error { ... }
```

### Step 3: `AutoPromoteLogRepo.ListForStopEvaluation()`

```go
// internal/repository/auto_promote_log_repo.go 新增（已完成）
func (r *AutoPromoteLogRepo) ListForStopEvaluation(limit int) ([]model.AutoPromoteLog, error) { ... }
```

### Step 4: `AutoStopService`

```go
// internal/service/auto_stop.go 核心逻辑
func (s *AutoStopService) evaluate(pl model.AutoPromoteLog) bool {
    order := orderRepo.GetByID(*pl.OrderID)

    // 1. 查策略获取关停系数
    strategy := strategyRepo.GetByID(pl.StrategyID)
    K := strategy.StopCoefficient  // 默认 0.7

    // 2. 解析基线对 [S0, S1] 从 stat_raw_data
    var baseline []model.VideoStat
    json.Unmarshal(pl.StatRawData, &baseline)
    S0, S1 := baseline[0], baseline[1]

    // 3. 查当前最新两条 video_stats
    //    author_video_id → export_id → GetLatestTwoByExportID
    Sn, Sn_1 := latestStats[0], latestStats[1]  // [newer, older]

    // 4. 规则一：增量率对比
    baselineDeltaPlay = S1.play - S0.play
    baselineDeltaLike = S1.like - S0.like
    baselineDeltaShare = S1.share - S0.share
    baselineLikeRate  = baselineDeltaLike / baselineDeltaPlay
    baselineShareRate = baselineDeltaShare / baselineDeltaPlay

    currentDeltaPlay = Sn.play - Sn_1.play
    currentDeltaLike = Sn.like - Sn_1.like
    currentDeltaShare = Sn.share - Sn_1.share
    currentLikeRate  = currentDeltaLike / currentDeltaPlay
    currentShareRate = currentDeltaShare / currentDeltaPlay

    if currentLikeRate / baselineLikeRate <= K
       OR currentShareRate / baselineShareRate <= K → 关停

    // 5. 规则二：吸粉成本（仅 followers）
    detail := weixinClient.GetPlanDetail(accountID, promotionID)
    if focus_num > 0 && (cost/10)/focus_num > 0.8 → 关停
}
```

### Step 5: `cmd/cron/auto-stop/main.go`

```go
// 模式与 cmd/cron/check-order/main.go 一致（已完成，需加 strategyRepo 依赖）
```

---

## 8. TODO（后续迭代）

| # | 项目 | 优先级 |
| --- | --- | --- |
| 1 | 飞书通知（关停时推消息，复用 NotifierService） | P1 |
| 2 | 关停后二次验证（再调 GetPlanDetail 确认状态） | P1 |
| 3 | 吸粉成本阈值 0.8 也抽到策略表 | P2 |
| 4 | 保护期可配置（30min） | P2 |
| 5 | Dashboard 关停统计展示 | P2 |

---

## 9. 风险与边界

| 风险 | 应对 |
| --- | --- |
| 投放初期数据少误关 | 保护期 ≥ 30min |
| `video_stats` 记录不足两条 | 跳过规则一 |
| 增量播放量 ≤ 0 | 跳过（无法计算增量率） |
| 基线增量率 ≤ 0 | 跳过对应指标（避免除零） |
| Stats Cron 未及时写入新数据 | 无新数据时自然不触发（安全保守） |
| 策略表无 stop_coefficient | GORM 默认值 0.7 兜底 |
| ClosePlan API 失败 | 不更新订单状态，下次轮询重试 |
| cost 字段为字符串 | ParseFloat 兜底 0 |
