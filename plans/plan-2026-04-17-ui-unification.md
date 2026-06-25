# UI 统一化与视觉升级改造

## 参考 UI

**TailwindAdmin** ([next-free.tailwind-admin.com](https://next-free.tailwind-admin.com))

核心设计特征提取：
- **主色**：柔和品牌蓝 `#5D87FF`，取代当前的 `#1E88E5`
- **页面背景**：淡灰蓝 `#F2F6FA`，令白色卡片更突出
- **大圆角**：卡片 `12px`、按钮 `8px`、头像/图标 `12px`
- **柔和阴影**：`0 2px 12px rgba(0,0,0,0.08)`，无硬边框
- **字体**：Inter（Google Fonts），取代系统默认字体栈
- **侧边栏**：白色背景 + 分类大写标题（HOME / PAGES），激活项圆角蓝底
- **表格**：无重边框、大行间距、圆形操作图标按钮
- **统计卡片**：彩色柔和圆角图标 + 粗数字，更具视觉冲击

---

## 需求概述

**业务目标**：将投放管理系统的 14 个页面视觉风格对齐 TailwindAdmin，提取公共组件，使整体视觉达到现代 SaaS 管理后台水平。

**现状问题**（逐页审计结果）：

| 问题类型 | 详细描述 | 影响页面 |
|---------|---------|---------|
| **配色/圆角老旧** | 主色 `#1E88E5` 偏暗，卡片 `8px` 圆角偏保守，阴影带 `1px border` 显生硬 | 全局 |
| **字体默认** | 使用系统字体栈，无 Google Fonts 现代字体 | 全局 |
| **侧边栏风格** | 深蓝 `#0D2137` 暗色侧边栏，无分类标题，激活态为左边框高亮 | base.html |
| **stat-card 平淡** | 图标偏小（44px），无大圆角柔和背景，数字冲击力不足 | dashboard |
| **inline style 泛滥** | drawer 详情页使用内联 style（如 `font-size:12px; color:#6b7280`），未复用 CSS class | accounts, orders, promote_logs, campaigns |
| **时间格式化函数重复** | `fmtDate()` / `fmtTime()` / `formatDate()` 每个页面各自 copy 一份 | 全部 14 页 |
| **pageList() 分页逻辑重复** | 每个带分页的页面均重复实现 `pageList()` / `goPage()` / `jumpPage()` | 7 页 |
| **分页 HTML 不统一** | orders/campaigns 的分页 HTML 与 tags/accounts 的分页 HTML 结构不同 | orders, campaigns vs tags, accounts |
| **表格行高不一致** | `td { padding: 20px 20px }` 行间距过大且不对称（当前值明显异常） | 全局 |
| **Drawer inline grid** | accounts 详情用 inline grid + inline color | accounts |
| **create.html 无 card 包裹** | 创建投放页面直接使用 step-card，无 card 容器 | create |
| **Toast 组件重复** | 每个页面都有 `<div class="toast">` 和 `showToast()` 函数 | 全部 |
| **StatusText 函数重复** | `statusText()` / `statusLabel()` 映射在多页面重复 | orders, campaigns, promote_logs, strategies |
| **操作按钮风格不一** | 有的用 `btn-ghost`，有的加 `style="color:red"` | accounts, settings |

---

## 技术方案

### 涉及模块

- **CSS**: `static/css/style.css` — 全面重写 CSS 变量 + 核心组件样式
- **字体**: base.html 引入 Google Fonts Inter
- **JS 公共工具**: 新建 `static/js/utils.js` 提取公共函数
- **前端 templates**: 14 个 HTML 模板逐一调整
- **后端**: 无变更

### 不涉及

- models / repo / service / handler 层无任何改动
- 不新增功能，纯 UI/UX 一致性优化

---

## 实现计划

### Step 1: CSS 设计系统重建 — 对齐 TailwindAdmin 视觉

**修改 `style.css` `:root` 变量**：

```css
/* 原始值 → TailwindAdmin 风格新值 */
--primary: #1E88E5       → #5D87FF
--primary-hover: #1565C0 → #4570EA
--primary-light: #E3F2FD → #ECF2FF
--primary-bg: rgba(...)  → rgba(93,135,255,0.08)
--bg-page: #f0f2f5       → #F2F6FA
--radius: 8px            → 12px
--radius-sm: 6px         → 8px
--shadow-card: 0 1px 3px rgba(0,0,0,0.06) → 0 2px 12px rgba(0,0,0,0.08)
--border: #e5e7eb        → #e5eaef  (边框可选去除，改用阴影分层)
```

**字体引入** (base.html `<head>`)：
```html
<link rel="preconnect" href="https://fonts.googleapis.com">
<link href="https://fonts.googleapis.com/css2?family=Inter:wght@400;500;600;700&display=swap" rel="stylesheet">
```

```css
body { font-family: 'Inter', -apple-system, ...; }
```

**卡片去边框**：TailwindAdmin 的卡片无 `border`，仅靠阴影区分层级：
```css
.card { border: none; border-radius: var(--radius); box-shadow: var(--shadow-card); }
```

---

### Step 2: 侧边栏改造 — 白底 + 分类标题

**目标**：从当前深蓝暗色侧边栏 → TailwindAdmin 风格的白色侧边栏。

**CSS 变量变更**：
```css
--sidebar-bg: #0D2137   → #FFFFFF
--sidebar-hover         → rgba(93,135,255,0.06)
--sidebar-active        → #ECF2FF (主色浅底)
```

**侧边栏结构调整**：
- 导航项文字颜色：由 `#8899aa` → `#2A3547`（深色文本）
- 激活态：由左侧 `border-left` → 整行 `border-radius: 8px` + `background: var(--sidebar-active)` + 文字蓝色
- 分类标题：增加 `.nav-category` class（如 `平台管理`、`投放管理`），使用 `12px` 大写灰色标题
- 底部分割线：由 `rgba(255,255,255,0.08)` → `var(--border)`
- 侧边栏右侧添加 `1px solid var(--border)` 分割

**base.html 结构微调**：
- 顶层分类文字样式从 `.nav-group-title` 分离，增加 `.nav-category` 用于分组标题
- Logo 颜色：`#fff` → `var(--primary)` + `-text-primary`

---

### Step 3: 表格视觉升级

**修改 `style.css` 表格区域**：

```css
/* 当前: td { padding: 20px 20px } — 明显过大 */
th { padding: 12px 16px; }       /* 表头加厚 */
td { padding: 14px 16px; }       /* 行高舒适但不浪费 */

/* 表头样式 */
thead { background: transparent; }  /* TailwindAdmin 表头无背景 */
th { color: var(--text-secondary); font-weight: 600; font-size: 13px; border-bottom: 1px solid var(--border); }

/* 行分割线更浅 */
td { border-bottom: 1px solid #f0f0f0; }
tbody tr:last-child td { border-bottom: none; }

/* hover 效果更柔和 */
tbody tr:hover { background: #F5F7FA; }
```

**操作列**：TailwindAdmin 使用圆角浅色底图标按钮，增加 `.btn-icon` class：
```css
.btn-icon {
  width: 32px; height: 32px; border-radius: 8px;
  display: inline-flex; align-items: center; justify-content: center;
  background: var(--primary-light); color: var(--primary);
  border: none; cursor: pointer; transition: all 0.2s;
}
.btn-icon:hover { background: var(--primary); color: #fff; }
.btn-icon.danger { background: #FEF2F2; color: #dc2626; }
.btn-icon.danger:hover { background: #dc2626; color: #fff; }
```

---

### Step 4: 统计卡片 (stat-card) 升级

**Dashboard stat-card 对齐 TailwindAdmin 风格**：
- 图标区域更大：`52px × 52px`，`border-radius: 12px`
- 彩色柔和背景更饱和
- 数字更粗大：`font-size: 26px; font-weight: 700`
- 卡片内边距增大：`padding: 20px`
- 可选：卡片底部增加细微渐变色条

```css
.stat-card { padding: 20px; gap: 16px; border: none; box-shadow: var(--shadow-card); }
.stat-icon { width: 52px; height: 52px; border-radius: 12px; }
.stat-value { font-size: 26px; font-weight: 700; }
```

---

### Step 5: 提取 JS 公共工具函数 → `static/js/utils.js`

**新建 `static/js/utils.js`**，提取各页面重复的纯函数：

```javascript
// 时间格式化（统一取代各页面的 fmtDate/fmtTime/formatDate）
function fmtDate(d) { ... }

// 数字千分位格式化
function fmtNum(n) { ... }

// 状态文本映射（统一取代各页面的 statusText/statusLabel）
function statusText(s) { ... }

// 分页号码列表生成（统一取代各页面的 pageList）
function pageList(currentPage, totalPages) { ... }
```

**影响范围**：base.html 引入 `<script src="/static/js/utils.js">` ，所有 14 页移除各自重复实现。

---

### Step 6: 统一分页器 HTML

**目标**：所有 7 个带分页的页面使用完全相同的 pagination HTML 结构：

```
pagination-left: 「共 N 条」 + pageSize 选择器
pagination-center: 「« ‹ [页码] › »」
pagination-right: 「跳至 ___ 页」
```

**分页样式升级**（对齐 TailwindAdmin "Page 1 of 2" + "Rows per page" 样式）：
```css
.page-btn { border-radius: var(--radius-sm); }  /* 更圆 */
.page-btn-active { background: var(--primary); border-radius: var(--radius-sm); }
```

**处理**：将 orders.html / campaigns.html 的分页 HTML 对齐为与 tags/accounts 相同的三段式结构。

---

### Step 7: 消除 Drawer inline style

**accounts.html 详情 drawer**：
- 将 `<div style="display:grid; grid-template-columns:1fr 1fr; ...">` 改为已有的 `.info-grid` class
- 将 `<div style="font-size:12px; color:#6b7280">` 改为 `.info-label` class
- 将 `<div style="font-size:14px; font-weight:500">` 改为 `.info-value` class

**影响文件**：accounts.html（L114-L170 全部改为 class 引用）

---

### Step 8: 统一操作列按钮风格

**规范**：
- 查看详情 / 编辑：`btn-icon`（蓝色圆角图标按钮，参考 TailwindAdmin 表格操作列）
- 删除 / 危险操作：`btn-icon danger`
- 文字按钮保留：`btn btn-ghost btn-sm`（辅助场景）

**影响文件**：accounts.html, settings.html, orders.html 等所有含操作列页面

---

### Step 9: create.html 包裹 card 容器

将 step-card 区域包裹在 `.card` 容器中：

```html
<div class="card">
  <div class="card-body">
    <!-- step cards -->
  </div>
</div>
```

---

### Step 10: 移除各页面重复 Toast 和 showToast

**方案**（推荐）：在 base.html 中添加全局 toast HTML + Alpine global store：
```html
<div class="toast" x-data x-show="$store.toast.msg" x-text="$store.toast.msg" x-transition x-cloak></div>
```

```javascript
// utils.js
Alpine.store('toast', { msg: '', show(m) { this.msg = m; setTimeout(() => this.msg = '', 3000); } });
```

各页面中 `showToast('xxx')` → `$store.toast.show('xxx')` ，并删除各自的 `<div class="toast">` 和 `showToast()` 方法。

---

### Step 11: Topbar / Header 升级

**对齐 TailwindAdmin 顶栏风格**：
- 高度：`56px` → `64px`
- 去除底部 `border-bottom`，改用阴影过渡
- Logo 居中或靠左（TailwindAdmin 为居中品牌名）
- 右侧元素：主题切换 (可选)、通知 (可选)、用户头像

```css
.topbar {
  height: 64px;
  border-bottom: none;
  box-shadow: 0 1px 4px rgba(0,0,0,0.06);
}
```

---

### Step 12: 按钮系统微调

**对齐 TailwindAdmin 更圆润的按钮**：
```css
.btn { border-radius: var(--radius-sm); }  /* 8px，更圆 */
.btn-primary { box-shadow: 0 2px 8px rgba(93,135,255,0.3); }  /* 按钮阴影 */
.btn-primary:hover { box-shadow: 0 4px 12px rgba(93,135,255,0.4); transform: translateY(-1px); }
```

---

### Step 13: 表单输入框升级

```css
.form-group input, .form-group select, .form-input {
  border-radius: var(--radius-sm);  /* 8px */
  padding: 10px 14px;              /* 更宽松 */
}
/* 焦点态使用主色 ring */
.form-group input:focus { box-shadow: 0 0 0 3px rgba(93,135,255,0.15); }
```

---

## 改动清单（按文件）

| 文件 | 改动内容 |
|------|---------|
| **style.css** | `:root` 变量全面重写、侧边栏白底化、表格/卡片/按钮/表单样式升级、新增 `.btn-icon` / `.nav-category` 等 class |
| **base.html** | 引入 Google Fonts Inter、引入 utils.js、侧边栏结构微调（增加分类标题、白底适配）、顶栏高度调整、全局 toast |
| **[NEW] static/js/utils.js** | 公共 fmtDate/fmtNum/statusText/pageList + Alpine toast store |
| **dashboard.html** | stat-card 图标/数字尺寸适配新 CSS |
| **accounts.html** | drawer inline→class，按钮→btn-icon，toast→global |
| **orders.html** | 分页补全，操作按钮统一，JS 去重，toast→global |
| **campaigns.html** | 分页补全，操作按钮统一，JS 去重，toast→global |
| **promote_logs.html** | JS 去重，toast→global |
| **tags.html** | JS 去重，toast→global |
| **videos.html** | JS 去重，toast→global |
| **video_stats.html** | JS 去重，toast→global |
| **strategies.html** | JS 去重，toast→global |
| **authors.html** | JS 去重，toast→global |
| **settings.html** | 按钮统一，toast→global |
| **create.html** | card 包裹，toast→global |
| **login.html** | 登录页配色适配新主色 |

---

## 视觉对比概要

| 维度 | 当前 | TailwindAdmin 目标 |
|------|------|-------------------|
| **主色** | `#1E88E5` (Material Blue) | `#5D87FF` (柔和品牌蓝) |
| **页面背景** | `#f0f2f5` (中性灰) | `#F2F6FA` (淡蓝灰) |
| **卡片** | `8px` 圆角 + `1px border` + 弱阴影 | `12px` 圆角 + 无边框 + 柔和阴影 |
| **字体** | 系统字体栈 | Inter (Google Fonts) |
| **侧边栏** | 深蓝 `#0D2137` 暗色 | 白色 `#FFFFFF` + 蓝色激活态 |
| **表格行高** | `td: 20px 20px` (过大) | `td: 14px 16px` (舒适) |
| **按钮** | 方形 `6px` 圆角 | 圆润 `8px` 圆角 + 阴影 |
| **操作按钮** | 文字 ghost 按钮 | 圆角彩色图标按钮 `.btn-icon` |
| **统计卡片** | 小图标 `44px` | 大图标 `52px` + 更粗数字 |
| **顶栏** | `56px` + border-bottom | `64px` + box-shadow |

---

## 边界与风险

- [x] **并发安全**：不涉及（纯前端改动）
- [x] **幂等性**：不涉及
- [x] **权限控制**：不涉及
- [x] **数据一致性**：不涉及
- [x] **兼容性**：所有改动向后兼容，无破坏性变更
- [ ] **性能瓶颈**：新增 Google Fonts Inter (~20KB) + utils.js (~2KB)，需确保字体用 `display=swap` 避免 FOIT
- [x] **错误处理**：不涉及
- [x] **数据迁移**：不涉及

### 风险点

1. **Google Fonts 可达性**：国内网络可能无法访问 Google Fonts。**备选**：使用 `fonts.loli.net` CDN 镜像或自托管 Inter woff2
2. **侧边栏白底化**：当前所有页面侧边栏文字/图标颜色基于深色背景设计，白底化需要反转全部颜色，影响 base.html + style.css 侧边栏区域约 60 行
3. **Alpine store 初始化顺序**：`utils.js` 中的 `Alpine.store()` 需要在 Alpine.js 加载后执行
4. **pageList 签名变更**：从 `this.pageList()` 改为 `pageList(this.page, this.totalPages)`，需仔细替换

---

## 验证计划

1. `go build ./...` 编译通过
2. 逐页打开 14 个页面，验证布局/分页/toast/按钮/drawer 正常
3. 视觉对比检查清单：
   - [ ] 卡片无边框、大圆角、柔和阴影
   - [ ] 侧边栏白底、蓝色激活态、分类标题
   - [ ] 表格行高舒适、hover 柔和、操作列图标按钮
   - [ ] 统计卡片大图标/粗数字
   - [ ] 顶栏 64px 无边框阴影过渡
   - [ ] 按钮圆润、主按钮带阴影
   - [ ] Inter 字体加载正常
4. 浏览器无 JS 控制台报错
5. Drawer 宽度均为 50%

---

## 实际变更（开发完成后由 /api-summarize 回填）

- 新增/修改的文件清单
- 测试覆盖率数据
- 遗留问题或后续优化项
