# Dashboard 完善计划 — 投放管理系统导航重构 & 数据面板

> 投放管理后台统一使用 **TableView 表格 + 基础 CRUD** 模式。
> 前端技术栈：Go Template + Alpine.js + Chart.js CDN，无构建步骤。

---

## 一、现有系统盘点

### 1.1 数据模型清单 (18 个 Model + 1 常量)

| Model | 表名 | 关键字段数 | 业务领域 | 页面 | API | CRUD |
| --- | --- | --- | --- | --- | --- | --- |
| User | users | 4 | 用户/鉴权 | ✅ login | ✅ | R |
| PlatformAccount | platform_accounts | 6 | 平台账号 | ✅ accounts | ✅ | CRUD ✅ |
| Author | authors | 10 | 作者 | ✅ authors | ✅ | R |
| AuthorVideo | author_videos | 10 | 作者视频 | ✅ videos | ✅ | R |
| AuthorPromoteStrategy | author_promote_strategies | 16 | 投放策略 | ✅ strategies | ✅ | CRUD ✅ |
| VideoStat | video_stats | 20 | 视频统计 | ✅ video_stats | ✅ | CRD |
| AutoPromoteLog | auto_promote_logs | 18 | 自动投放日志 | ✅ promote_logs | ✅ | R+确认 |
| Campaign | campaigns | 8 | 活动/投放 | ✅ campaigns | ✅ | R+导出 |
| Order | orders | 14 | 订单 | ✅ orders | ✅ | R+提交 |
| PerformanceLog | performance_logs | 5 | 效果追踪 | ❌ | ❌ | 无 |
| PromptTemplate | prompt_templates | 4 | 提示词 | ✅ settings | ✅ | CRUD ✅ |
| ZhugeTag | zhuge_tags | 6 | 标签 | ✅ tags | ✅ | R+同步 |
| AuditLog | audit_logs | 4 | 操作审计 | ❌ | ❌ | 无 |
| FeishuFolder | feishu_folders | 6 | 飞书文件夹 | — | — | 内部 |
| FeishuSpreadsheet | feishu_spreadsheets | 5 | 飞书表格 | — | — | 内部 |
| FeishuSheetTab | feishu_sheet_tabs | 6 | 飞书Sheet | — | — | 内部 |
| FeishuSyncCursor | feishu_sync_cursors | 4 | 同步游标 | — | — | 内部 |
| Platform (常量) | — | — | 平台定义 | — | — | — |

### 1.2 已实现的二级导航 (6 组)

```text
├── 📊 面板管理        → 统计面板 /
├── 🖥️ 平台管理       → 账号管理 /accounts-page
├── 👥 作者管理        → 作者 / 视频 / 策略 / 视频统计
├── 🚀 投放管理        → 创建投放 / 活动 / 订单 / 日志
├── 🏷️ 标签管理       → 标签管理 /tags-page
└── ⚙️ 其他设置       → 提示词 /prompts-page
```

### 1.3 实施完成清单

| 项目 | 状态 | 说明 |
| --- | --- | --- |
| 二级折叠导航 (base.html) | ✅ 完成 | Alpine.js 手风琴，ActiveGroup 控制展开 |
| PageData + ActiveGroup | ✅ 完成 | render.go + page.go 全量传递 |
| 视频管理页 (videos.html) | ✅ 完成 | TableView 基础骨架 |
| 视频统计页 (video_stats.html) | ✅ 完成 | TableView + 4 维筛选 |
| 标签管理页 (tags.html) | ✅ 完成 | TableView + 同步按钮 |
| Dashboard 增强 (12 卡片) | ✅ 完成 | 订单维度 6 + 新增维度 6 |
| DashboardHandler 扩展 (7 repo) | ✅ 完成 | Overview API |
| TagHandler (List/Tree/Sync) | ✅ 完成 | — |
| AuthorVideoHandler (List/Get/Stats) | ✅ 完成 | — |
| Repo 增强 (6 文件) | ✅ 完成 | ListPaged/Count/ListFiltered/TodayCount等 |
| Router + main.go 装配 | ✅ 完成 | go build + go vet 通过 |
| **Phase 4-0: URL 路由分页** | ✅ 完成 | 5 个页面全部支持 URL 分页 |
| 全局 CSS 增强 (style.css) | ✅ 完成 | 分页器升级+数字对齐+吸顶+悬停+空状态+kebab |
| videos.html 美化 | ✅ 完成 | 完整分页器+封面fallback+加载态+重置 |
| video_stats.html 美化 | ✅ 完成 | 列数精简+parseFloat修复+kebab+抽屉增强 |
| tags.html 美化 | ✅ 完成 | 完整分页器+空状态+加载态+重置 |
| promote_logs.html URL分页 | ✅ 完成 | initFromURL+syncURL |

---

## 二、Model 字段展示审查

> 逐表审查：Model 全部字段 vs 当前页面展示列，标记遗漏和修复方案。

### 2.1 `video_stats` (20 个字段)

| 字段 | 类型 | 当前列表 | 当前详情 | 建议 |
| --- | --- | --- | --- | --- |
| id | int64 | ✅ | ✅ | — |
| author_video_id | int64 | ❌ | ❌ | 抽屉详情 |
| platform | varchar(32) | ❌ **遗漏** | ❌ | ✅ 列表列 + 筛选 |
| export_id | varchar(256) | ❌ | ✅ | 保持详情 |
| collect_date | varchar(20) | ✅ | ✅ | — |
| author_id | *int64 | ✅ (昵称) | ❌ | — |
| description | text | ✅ 截断 | ✅ | — |
| publish_date | varchar(20) | ❌ **遗漏** | ❌ | ✅ 抽屉详情 |
| completion_rate | varchar(20) | ✅ | ✅ | ⚠️ 前端需 parseFloat() |
| avg_play_duration | varchar(20) | ❌ **遗漏** | ❌ | ✅ 抽屉详情 |
| play_count | int64 | ✅ | ✅ | — |
| recommend_count | int64 | ❌ **遗漏** | ❌ | ✅ 抽屉详情 |
| like_count | int64 | ✅ | ✅ | — |
| comment_count | int64 | ✅ | ✅ | — |
| share_count | int64 | ✅ | ✅ | — |
| follow_count | int64 | ✅ | ✅ | — |
| forward_count | int64 | ❌ **遗漏** | ❌ | ✅ 抽屉详情 |
| ringtone_count | int64 | ❌ | ❌ | 低优，抽屉可选 |
| status_count | int64 | ❌ | ❌ | 低优，抽屉可选 |
| cover_count | int64 | ❌ | ❌ | 低优，抽屉可选 |
| feishu_synced | bool | ✅ | ❌ | — |
| nonce | varchar(256) | ❌ | ❌ | 内部字段，不展示 |

**修复项**：
- 列表增加 `platform` 列 + 平台筛选
- `completion_rate` 前端改用 `parseFloat()` 再格式化
- 抽屉增加：`publish_date`, `avg_play_duration`, `recommend_count`, `forward_count`
- 抽屉增加折叠区：`ringtone_count`, `status_count`, `cover_count`（低优可选展示）

### 2.2 `author_videos` (10 个字段)

| 字段 | 类型 | 当前列表 | 当前详情 | 建议 |
| --- | --- | --- | --- | --- |
| id | int64 | ✅ | ✅ | — |
| author_id | int64 | ✅ (昵称) | ✅ | — |
| account_id | int64 | ❌ **遗漏** | ❌ | ✅ 列表(账号名) |
| export_id | varchar(256) | ✅ 截断 | ✅ | — |
| description | text | ✅ 截断 | ✅ | — |
| cover_url | text | ✅ 缩略图 | ✅ 大图 | — |
| publish_time | varchar(32) | ✅ | ✅ | — |
| nonce | varchar(256) | ❌ | ❌ | 内部字段，不展示 |
| raw_data | json | ❌ | ❌ | ✅ 抽屉增加 JSON 查看 |
| last_checked_at | *datetime | ❌ **遗漏** | ❌ | ✅ 抽屉详情(运维) |
| created_at | datetime | ✅ | ✅ | — |

**修复项**：
- 列表增加「账号」列 (account_id → account name JOIN)
- 抽屉增加 `last_checked_at` (最后采集检查时间)
- 抽屉增加 `raw_data` JSON 格式化查看（可折叠）

### 2.3 `authors` (10 个字段)

| 字段 | 类型 | 当前列表 | 建议 |
| --- | --- | --- | --- |
| id | int64 | ❌ (无列) | 可选 |
| account_id | int64 | ✅ (select) | — |
| username | varchar(256) | ✅ | — |
| nickname | varchar(256) | ✅ | — |
| avatar_url | text | ✅ | — |
| platform | varchar(32) | ✅ | — |
| raw_data | json | ❌ | 低优 |
| feishu_sync_enabled | bool | ❌ **遗漏** | ✅ 增加飞书标记列 |
| feishu_folder_token | *varchar | ❌ | 内部，不展示 |
| cached_at | datetime | ✅ | — |

**增强项**：
- 增加「飞书同步」列 (feishu_sync_enabled 标记)
- 增加「视频数」列 (COUNT author_videos)
- 增加「策略状态」列 (已配置/未配置/已禁用)

### 2.4 `zhuge_tags` — 字段全覆盖 ✅

所有 6 个字段均已在列表中展示，无遗漏。

### 2.5 Dashboard 统计卡片 — 字段审查

| 卡片 | 数据源 | 状态 |
| --- | --- | --- |
| 活动总数 | campaigns COUNT | ✅ |
| 投放中 | orders active | ✅ |
| 待支付 | orders pending | ✅ |
| 审核中 | orders review | ✅ |
| 失败 | orders failed | ✅ |
| 总消耗 | orders latest_detail→cost | ✅ |
| 作者总数 | authors COUNT | ✅ |
| 视频总数 | author_videos COUNT | ✅ |
| 今日采集 | video_stats today | ✅ |
| 策略启用中 | strategies enabled | ✅ |
| 今日投放 | promote_logs today | ✅ |
| 待审核投放 | promote_logs detected | ✅ |

**图表增强（待实施）**：

| 图表 | 数据源 | 优先级 |
| --- | --- | --- |
| 投放效果趋势 (7天) | performance_logs | P1 |
| 自动投放状态分布 | auto_promote_logs GROUP BY status | P1 |
| 作者投放排行 TOP 10 | auto_promote_logs JOIN authors | P2 |
| 视频热度排行 TOP 10 | video_stats latest per export | P2 |

---

## 三、URL 路由分页规范

> **核心需求**：所有列表页的分页和筛选条件必须同步到 URL query string，方便直接粘贴链接分享定位到具体页面。

### 3.1 标杆实现 — `orders.html` 模式

`orders.html` 已经实现了完整的 URL 路由分页，其他页面须对齐此标准：

```javascript
// ── 标杆代码 (orders.html) ──

// 1. init 时从 URL 恢复状态
async init() {
  const params = new URLSearchParams(window.location.search);
  if (params.get('page')) this.page = Math.max(1, parseInt(params.get('page')) || 1);
  if (params.get('status')) this.filterStatus = params.get('status');
  await this.loadOrders();
},

// 2. 每次加载后同步 URL
syncURL() {
  const params = new URLSearchParams();
  params.set('page', this.page);
  if (this.filterStatus) params.set('status', this.filterStatus);
  history.replaceState(null, '', window.location.pathname + '?' + params.toString());
},

// 3. load 完成后调用 syncURL
async loadOrders() {
  // ... fetch data ...
  this.syncURL();
},

// 4. 统一跳页方法
goPage(p) { if (p >= 1 && p <= this.totalPages) { this.page = p; this.load(); } },
```

### 3.2 需要适配的 4 个页面

| 页面 | URL 格式 | 需同步的参数 |
| --- | --- | --- |
| videos.html | `/videos-page?page=2&author_id=5` | page, author_id |
| video_stats.html | `/video-stats-page?page=3&author_id=5&date_from=2026-03-01&feishu_synced=true` | page, author_id, date_from, date_to, feishu_synced |
| tags.html | `/tags-page?page=2&keyword=美妆&level=2&parent_id=abc` | page, keyword, level, parent_id |
| promote_logs.html | `/promote-logs-page?page=1&author_id=3&video_keyword=直播` | page, author_id, video_keyword |

### 3.3 统一实现规范

每个 TableView 页面必须实现以下 3 个方法：

```javascript
// ── 统一规范 ──

// 1. initFromURL() — 在 init() 开头调用
initFromURL() {
  const params = new URLSearchParams(window.location.search);
  this.page = Math.max(1, parseInt(params.get('page')) || 1);
  // 各页面补充自己的 filters
  if (params.get('author_id')) this.filters.author_id = params.get('author_id');
  // ... 其他 filter 字段
},

// 2. syncURL() — 在 load() 成功后调用
syncURL() {
  const params = new URLSearchParams();
  params.set('page', this.page);
  // 仅写入有值的筛选条件（空值不上 URL）
  Object.entries(this.filters).forEach(([k, v]) => { if (v) params.set(k, v); });
  history.replaceState(null, '', window.location.pathname + '?' + params.toString());
},

// 3. goPage(p) — 统一跳页
goPage(p) { if (p >= 1 && p <= this.totalPages) { this.page = p; this.load(); } },
```

---

## 四、数据展示美化规范

> 参考现代管理系统设计最佳实践，统一提升数据展示体验。

### 4.1 表格数据对齐规则

| 数据类型 | 对齐方式 | CSS | 示例 |
| --- | --- | --- | --- |
| 文本 (名称/描述) | 左对齐 | `text-align: left` | 作者名、视频描述 |
| 数字 (播放/点赞) | **右对齐** + 等宽字体 | `text-align: right; font-variant-numeric: tabular-nums` | 12,345 |
| ID | 左对齐 + monospace | `font-family: monospace; font-size: 12px` | #1234 |
| 状态标签 | 居中 | `text-align: center` | ✅已同步 |
| 时间 | 右对齐 | `text-align: right; color: #9ca3af` | 04/02 12:30 |
| 操作列 | 右对齐 | `text-align: right; white-space: nowrap` | ⋮ 菜单 |

新增 CSS 类：

```css
/* 数字列右对齐 */
td.num, th.num { text-align: right; font-variant-numeric: tabular-nums; }
td.num { font-weight: 500; }
/* ID 列 */
td.id-col { font-size: 12px; font-family: monospace; color: #6b7280; }
/* 操作列 */
td.actions { text-align: right; white-space: nowrap; }
```

### 4.2 分页器升级

当前分页器过于简陋（仅上一页/下一页），升级为完整分页器：

```text
┌──────────────────────────────────────────────────────────────┐
│  共 1,234 条  │  每页 [20▾]  │  ‹  1 2 3 ... 62  ›  │  跳至 [__] 页  │
└──────────────────────────────────────────────────────────────┘
```

功能要求：
- **总数显示**：`共 X 条` 始终可见
- **每页条数选择**：下拉 `[20, 50, 100]`
- **页码按钮**：显示首页、末页、当前页前后各 2 页 + 省略号
- **快速跳转**：输入框 + Enter 跳转
- **键盘快捷键**：← → 翻页

### 4.3 表格增强样式

| 优化项 | 实现 |
| --- | --- |
| **悬停行高亮** | `tbody tr:hover` 增加左侧 3px accent border |
| **斑马纹** | `tbody tr:nth-child(even)` 轻微背景色 |
| **表头吸顶** | `thead { position: sticky; top: 60px; z-index: 10; }` |
| **列宽控制** | ID 列 60px、描述列 200-280px、操作列 auto |
| **数字千分位** | 统一用 `.toLocaleString()` 格式化 |
| **截断提示** | 长文本增加 `title` 属性 + `text-overflow: ellipsis` |

新增 CSS：

```css
/* 悬停行增强 */
tbody tr { border-left: 3px solid transparent; transition: all 0.15s; }
tbody tr:hover { border-left-color: #6366f1; background: #f9fafb; }

/* 斑马纹 */
tbody tr:nth-child(even) { background: #fafbfc; }
tbody tr:nth-child(even):hover { background: #f3f4f6; }

/* 表头吸顶 */
.table-wrapper { overflow-x: auto; max-height: calc(100vh - 280px); overflow-y: auto; }
.table-wrapper thead { position: sticky; top: 0; z-index: 10; background: #f9fafb; }

/* 列宽控制 */
th.col-id, td.col-id { width: 60px; }
th.col-desc, td.col-desc { max-width: 280px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
th.col-actions, td.col-actions { width: auto; text-align: right; white-space: nowrap; }
```

### 4.4 空状态美化

替换当前纯文字 `暂无数据` 为带 SVG 插画 + 引导文案的空状态组件：

```html
<div class="empty-state">
  <svg class="empty-icon" viewBox="0 0 120 120">...</svg>
  <div class="empty-title">暂无视频数据</div>
  <div class="empty-desc">数据由定时任务自动采集，请稍后刷新</div>
</div>
```

```css
.empty-state { text-align: center; padding: 60px 20px; }
.empty-icon { width: 80px; height: 80px; opacity: 0.4; margin-bottom: 16px; }
.empty-title { font-size: 15px; font-weight: 600; color: #6b7280; margin-bottom: 6px; }
.empty-desc { font-size: 13px; color: #9ca3af; }
```

### 4.5 行操作按钮优化

当操作按钮超过 2 个时，收敛为 kebab 菜单 `⋮`：

```html
<td class="actions">
  <div class="action-menu" x-data="{ open: false }">
    <button @click="open = !open" class="btn-kebab">⋯</button>
    <div x-show="open" @click.outside="open = false" class="action-dropdown">
      <a @click="openView(item)">查看详情</a>
      <a @click="openStats(item)">查看统计</a>
      <hr>
      <a @click="remove(item)" class="danger">删除</a>
    </div>
  </div>
</td>
```

### 4.6 筛选栏统一增加「重置」按钮

所有筛选栏末尾增加重置按钮，一键清空所有筛选条件并回到第 1 页：

```javascript
resetFilters() {
  this.filters = { /* 所有字段置空 */ };
  this.page = 1;
  this.load();
}
```

### 4.7 加载状态增强

**表格 loading 态**：加载中显示半透明遮罩 + spinner，而非隐藏表格：

```css
.table-loading-overlay {
  position: absolute; inset: 0;
  background: rgba(255,255,255,0.7);
  display: flex; align-items: center; justify-content: center;
  z-index: 5;
}
```

### 4.8 数字格式化工具函数

所有页面统一使用 `fmtNum()` 格式化数字：

```javascript
// 统一数字格式化
fmtNum(n) {
  if (n == null || n === '') return '-';
  return Number(n).toLocaleString('zh-CN');
},
fmtRate(r) {
  if (r == null || r === '') return '-';
  const v = parseFloat(r);
  if (isNaN(v)) return r; // 已经是字符串格式如 "45.2%"
  return (v * 100).toFixed(1) + '%';
},
```

---

## 五、UI 优化清单（按页面）

### 5.1 video_stats.html 优化

| 优化项 | 说明 | 优先级 |
| --- | --- | --- |
| **URL 路由分页** | ✅ 已完成 | — |
| **列数精简 (12→10列)** | ✅ 已完成 | 去掉分享/关注列，放入抽屉详情 |
| **增加平台列+筛选** | model 有 platform 字段但未使用 | P1 |
| **completion_rate 类型修复** | ✅ 已完成 parseFloat | — |
| **数字右对齐+千分位** | ✅ 已完成 fmtNum | — |
| **抽屉增强** | ✅ 已完成 增加 publish_date/avg_play_duration/recommend/forward | — |
| **重置按钮** | ✅ 已完成 | — |
| **互动数据格网布局** | ✅ 已完成 metric-grid 3×2 布局 | — |

### 5.2 videos.html 优化

| 优化项 | 说明 | 优先级 |
| --- | --- | --- |
| **URL 路由分页** | ✅ 已完成 | — |
| **增加平台筛选** | filter-bar 增加 platform select | P1 |
| **重置按钮** | ✅ 已完成 | — |
| **封面图 fallback** | ✅ 已完成 首字占位 | — |
| **增加账号列** | account_id → account_name JOIN | P2 |
| **抽屉增强** | 增加 last_checked_at / raw_data JSON 查看器(可折叠) | P2 |

### 5.3 tags.html 优化

| 优化项 | 说明 | 优先级 |
| --- | --- | --- |
| **URL 路由分页** | ✅ 已完成 | — |
| **重置按钮** | ✅ 已完成 | — |

### 5.4 promote_logs.html 优化

| 优化项 | 说明 | 优先级 |
| --- | --- | --- |
| **URL 路由分页** | ✅ 已完成 | — |

### 5.5 authors.html 优化

| 优化项 | 说明 | 优先级 |
| --- | --- | --- |
| **增加飞书同步标记列** | feishu_sync_enabled 标记 icon | P1 |
| **增加视频数列** | COUNT(author_videos) | P1 |
| **增加策略状态列** | 查 strategies 表 → 已配置/未配置/已禁用 | P1 |

### 5.6 dashboard.html 优化

| 优化项 | 说明 | 优先级 |
| --- | --- | --- |
| **Chart.js 图表区** | 引入 CDN 后实现 4 个图表 | P1 |
| **卡片点击跳转** | 点击"作者总数"跳转 /authors-page 等 | P2 |
| **实时刷新频率调优** | 30s→60s (减少 DB 压力) | P2 |

### 5.7 全局样式优化

| 优化项 | 影响范围 | 优先级 |
| --- | --- | --- |
| **数字右对齐 CSS** | ✅ 已完成 | — |
| **分页器升级** | ✅ 已完成 | — |
| **表头吸顶** | ✅ 已完成 | — |
| **悬停行增强** | ✅ 已完成 | — |
| **空状态美化** | ✅ 已完成 | — |
| **加载遮罩** | ✅ 已完成 | — |
| **行操作 kebab 菜单** | ✅ 已完成 video_stats | — |

### 5.8 promote_logs.html (标杆页面)

已经实现很精致的详情抽屉，其他新页面应对齐此标准：
- 分组 section 布局 (基本信息/视频信息/数据指标/投放关联/时间线/错误)
- 指标对比卡片 (metric-card + 图标)
- 时间线 (timeline-dot + timeline-line)

---

## 六、实施优先级排序

### Phase 4-0: URL 路由分页 (P0) ✅ 全部完成

1. ✅ **style.css** — 全局样式升级 (+130行)
2. ✅ **videos.html** — URL 路由分页 + 完整分页器 + 重置 + 封面 fallback
3. ✅ **video_stats.html** — URL 路由分页 + parseFloat + 列数精简 + kebab + 抽屉增强
4. ✅ **tags.html** — URL 路由分页 + 完整分页器 + 重置
5. ✅ **promote_logs.html** — URL 路由分页

### Phase 4a: 字段遗漏修复 (P1)

6. **video_stats.html** — 平台列 + 筛选，列数精简
7. **video_stats.html 抽屉** — 增加 publish_date / avg_play_duration / recommend_count / forward_count
8. **videos.html** — 平台筛选下拉
9. **authors.html** — 飞书同步列 / 视频数列 / 策略状态列

### Phase 4b: UI 体验优化 (P2)

10. Dashboard 数字 count-up 动画
11. videos.html 抽屉增强 (last_checked_at + raw_data JSON)
12. Dashboard 卡片点击跳转
13. Dashboard 刷新频率调优 30s→60s

### Phase 4c: Dashboard 图表 (P1-P2)

15. base.html 引入 Chart.js CDN
16. Dashboard API: promote-trend / promote-status-dist
17. Dashboard API: author-rank / video-hot
18. dashboard.html 图表渲染区

### Phase 5: 可选增强

19. PerformanceLog 页面 + API
20. AuditLog 查看页
21. 深色模式 / 响应式

---

## 五、完整 API 清单

### Dashboard API

| # | Method | Path | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 1 | GET | `/api/dashboard/overview` | ✅ 完成 | 12 维全局统计 |
| 2 | GET | `/api/dashboard/stats` | ✅ 兼容 | 重定向到 overview |
| 3 | GET | `/api/dashboard/recent-orders` | ✅ 完成 | 最新 N 订单 |
| 4 | GET | `/api/dashboard/promote-trend` | ❌ 待建 | 投放效果趋势 |
| 5 | GET | `/api/dashboard/promote-status-dist` | ❌ 待建 | 状态分布 |
| 6 | GET | `/api/dashboard/author-rank` | ❌ 待建 | 作者投放排行 |
| 7 | GET | `/api/dashboard/video-hot` | ❌ 待建 | 视频热度排行 |

### 视频管理 API

| # | Method | Path | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 8 | GET | `/author-videos` | ✅ 完成 | 列表 (分页+筛选) |
| 9 | GET | `/author-videos/:id` | ✅ 完成 | 详情 |
| 10 | GET | `/author-videos/:id/stats` | ✅ 完成 | 关联统计 |

### 视频统计 API

| # | Method | Path | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 11 | GET | `/video-stats` | ✅ 增强 | 4 维筛选 |
| 12 | GET | `/video-stats/:id` | ✅ 完成 | 详情 |
| 13 | POST | `/video-stats` | ✅ 完成 | 创建 |
| 14 | DELETE | `/video-stats/:id` | ✅ 完成 | 删除 |
| 15 | GET | `/video-stats/trend/:export_id` | ❌ 待建 | 统计趋势 |

### 标签管理 API

| # | Method | Path | 状态 | 说明 |
| --- | --- | --- | --- | --- |
| 16 | GET | `/tags` | ✅ 完成 | 列表 |
| 17 | GET | `/tags/tree` | ✅ 完成 | 树形 |
| 18 | POST | `/tags/sync` | ✅ 完成 | 同步 |

### 页面路由

| # | Path | 模板 | 状态 |
| --- | --- | --- | --- |
| 19 | `/videos-page` | videos.html | ✅ 完成 |
| 20 | `/video-stats-page` | video_stats.html | ✅ 完成 |
| 21 | `/tags-page` | tags.html | ✅ 完成 |

---

## 八、变更记录

### v1 (初始计划)

- 18 个 Model 盘点、GAP 分析
- 二级导航架构设计
- 统一 TableView 规范
- 5 阶段路线图

### v2 (实施完成 + 审查)

- Phase 1-3 全部实施完成（导航+Dashboard+视频+标签）
- 全表字段审查：`video_stats` 遗漏 6 字段、`author_videos` 遗漏 3 字段、`authors` 遗漏 3 列
- UI 优化清单：6 大类 16 个具体优化项
- video_stats completion_rate 类型 bug 标记
- 以 promote_logs.html 为 UI 标杆对齐标准
- 重排优先级：P1 字段修复 → P2 体验优化 → 图表增强

### v3 (数据展示美化 + URL 路由分页)

- 新增第三节：URL 路由分页规范（标杆代码 + 4 页面适配清单）
- 新增第四节：数据展示美化规范 8 项（数字对齐/分页器升级/表头吸顶/悬停增强/空状态/kebab/加载态/工具函数）
- 参考现代管理系统设计：数字右对齐、等宽数字字体、行 accent border 悬停、表头吸顶
- 分页器升级为完整版：总条数 + 每页选择 + 页码按钮 + 快速跳转
- 实施优先级重排：P0 URL 路由分页 → P1 字段修复 → P2 体验优化 → 图表增强
- `orders.html` 确认为路由分页标杆实现，其他 4 个页面须对齐

### v4 (Phase 4-0 完成回填)

- **Phase 4-0 全部完成** ✅ — 5 个页面 URL 路由分页 + 全局 CSS 大幅增强
- style.css 新增 +130 行：完整分页器、数字右对齐、表头吸顶、悬停行 accent、斑马纹、空状态、加载遮罩、kebab 菜单、badge
- video_stats.html 超额完成：列数精简(12→10)、completion_rate parseFloat 修复、kebab 菜单、抽屉增加 6 个数据字段 (metric-grid 布局)
- videos.html 超额完成：封面 fallback 首字占位
- 所有页面统一使用 fmtNum()/fmtRate() 格式化工具函数
- Phase 4a 部分超额完成：video_stats 抽屉增强(7/9)、video_stats 列数精简、completion_rate 修复
- Phase 4b 部分超额完成：空状态 SVG、加载遮罩、kebab 菜单
- 剩余待做：videos/video_stats 平台筛选列、authors 增强列、Dashboard 图表
