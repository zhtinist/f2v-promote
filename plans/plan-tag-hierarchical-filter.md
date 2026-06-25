# 标签分级筛选重构计划

## 1. 背景与现状

### 当前流程

```text
视频口播文案 → GPT（单次调用，传入全部二级标签）→ 返回标签组
```

**问题**：

- `GetFlatTags()` 返回所有标签（不分层级），扁平化为 `map[text]id`
- `GenerateZhugeTags()` 将所有标签名拼接传入 prompt，截断到 500 个
- GPT 需要从数百个无分类的标签中盲选，精度差且 token 消耗大
- 没有利用已构建的 **一级分类 → 二级标签** 层级结构

### 数据结构（已完成迁移）

```sql
-- 主标签表
zhuge_tags:
├── id          (varchar, 标签 ID)
├── text        (varchar, 标签名称)
├── parent_id   (varchar, 主分类 ID，一级为 NULL)
├── parent_text (varchar, 主分类名称)
├── level       (int, 1=一级分类, 2=二级标签)
└── updated_at  (datetime)

-- 多对多关联表（一个标签可归属 1~3 个分类）
zhuge_tag_categories:
├── tag_id      (varchar, 二级标签 ID)
└── category_id (varchar, 一级分类 ID)
```

**数据现状**（已通过 `cmd/tag-classify` 脚本完成）：

- 23 个一级分类（`level=1`）：音乐、运动、车、萌宠、舞蹈、美食、科技、知识、时尚、游戏 等
- ~26000 个二级标签（`level=2`），每个标签关联 1~3 个一级分类
- 多对多关系存储在 `zhuge_tag_categories` 关联表中

---

## 2. 目标设计

### 新流程（两步筛选）

```text
                    ┌──────────────────┐
  视频口播文案 ────→ │ Step 1: GPT 筛选  │
                    │ 一级分类（3~5个） │
                    └────────┬─────────┘
                             │
                    ┌────────▼─────────┐
                    │ 从 关联表 JOIN   │
                    │ 查询命中分类下的  │
                    │ 所有二级标签      │
                    └────────┬─────────┘
                             │
                    ┌────────▼──────────┐
                    │ Step 2: GPT 从    │
                    │ 筛选后的二级标签中 │
                    │ 精选标签组         │
                    └──────────────────┘
```

**优势**：

1. Step 1 仅传入 ~23 个一级分类名称，token 极少
2. Step 2 仅传入命中分类下的二级标签（通过关联表覆盖交叉分类），精度大幅提升
3. 多对多关联确保一个标签属于多个分类时不会被遗漏

---

## 3. 三个任务

### 任务 1：构建一级分类查询能力

**目标**：`ZhugeTagRepo` 新增方法，支持按层级和关联表查询标签。

**改动文件**：`internal/repository/tag_repo.go`

**新增方法**：

```go
// GetCategories 获取所有一级分类标签 (level=1)
func (r *ZhugeTagRepo) GetCategories() ([]model.ZhugeTag, error) {
    var tags []model.ZhugeTag
    err := r.db.Where("level = 1").Order("text ASC").Find(&tags).Error
    return tags, err
}

// GetChildrenByCategoryIDs 通过关联表获取分类下所有二级标签（多对多）
func (r *ZhugeTagRepo) GetChildrenByCategoryIDs(categoryIDs []string) ([]model.ZhugeTag, error) {
    var tags []model.ZhugeTag
    err := r.db.Distinct().
        Joins("JOIN zhuge_tag_categories tc ON zhuge_tags.id = tc.tag_id").
        Where("tc.category_id IN ?", categoryIDs).
        Find(&tags).Error
    return tags, err
}
```

**关键设计**：

- `GetCategories()` 直接查 `level=1`
- `GetChildrenByCategoryIDs()` 通过 `zhuge_tag_categories` 关联表 JOIN 查询，确保交叉分类标签被覆盖
- 使用 `Distinct()` 去重，避免同一标签因多个分类关联被重复返回

---

### 任务 2：一级分类筛选提示词

**目标**：新增 `zhuge_categories` prompt，让 GPT 根据视频文案选出 3~5 个相关的一级分类。

**Prompt Key**：`zhuge_categories`

**提示词内容**：

```text
你是一个视频号投放定向专家。根据以下视频口播文案，从平台兴趣标签的一级分类中选出最相关的 {count} 个分类。

平台一级分类列表：
{categories}

要求：
1. 选择与文案内容最相关的 {count} 个一级分类
2. 优先选择与视频主题直接相关的分类
3. 可以适当选择潜在受众感兴趣的关联分类
4. 只返回 JSON 数组，包含选中分类的名称

口播文案：
{script}

请直接返回 JSON 字符串数组，如 ["汽车", "科技", "财经"]：
```

**变量**：

| 变量 | 说明 |
| --- | --- |
| `{count}` | 选择分类数量（默认 5，可通过 `TAG_CATEGORY_COUNT` 配置） |
| `{categories}` | 所有一级分类名称，逗号分隔 |
| `{script}` | 视频口播文案 |

**返回格式**：`["分类A", "分类B", "分类C"]`

---

### 任务 3：二级标签精选提示词

**目标**：改造现有 `zhuge_tags` prompt，传入的标签池从全量改为仅限筛选后的二级标签，并注明所属分类。

**Prompt Key**：`zhuge_tags`（沿用，更新内容）

**提示词内容**：

```text
你是一个视频号加热投放专家。根据以下视频口播文案，生成 {count} 组差异化的投放定向标签。

**重要：兴趣标签必须从以下平台标签列表中选择，不能自己编造。**

平台可用标签（按分类组织）：
{available_tags}

每组标签包含：
- name: 标签组简短描述（如"年轻女性美妆兴趣"）
- interest_tag: 从上面平台标签中选择 1 个最匹配的标签名（必须完全匹配列表中的文字）
- ages: 年龄段数组，从 [1,2,3,4,5] 中选择子集（1=18-24岁, 2=25-29岁, 3=30-39岁, 4=40-49岁, 5=50岁以上）
- sex: 性别（null=不限, 1=男, 2=女）
- cityIds: 地域 ID 列表（空数组=全国投放）

要求：
1. {count} 组标签之间要有明显差异，覆盖不同人群
2. 结合文案内容推断目标受众
3. interest_tag 必须从平台标签列表中精确选择
4. 只返回 JSON 数组，不要其他内容

口播文案：
{script}

请直接返回 JSON 数组：
```

**改动点**（与旧 prompt 对比）：

`{available_tags}` 格式从平铺逗号分隔，改为**按分类分组**展示：

```text
【汽车】: 新能源汽车, 燃油车, 二手车, 汽车用品
【科技】: 人工智能, 半导体, 5G通信, 消费电子
【财经】: 股票基金, 保险理财, 房产投资
```

分类上下文帮助 GPT 更精准理解标签含义和投放场景。

---

## 4. 代码改动清单

| # | 文件 | 操作 | 描述 |
| --- | --- | --- | --- |
| 1 | `internal/repository/tag_repo.go` | 新增 | `GetCategories()` + `GetChildrenByCategoryIDs()` |
| 2 | `internal/service/openai.go` | 新增 | `defaultCategoryPrompt` 常量 |
| 3 | `internal/service/openai.go` | 新增 | `filterCategories()` 方法（Step 1 GPT 调用） |
| 4 | `internal/service/openai.go` | 重构 | `GenerateZhugeTags()` 改为两步调用流程 |
| 5 | `internal/service/openai.go` | 更新 | `defaultZhugePrompt` 中 `{available_tags}` 格式说明 |
| 6 | `internal/service/openai.go` | 更新 | 构造器注入 `ZhugeTagRepo` |
| 7 | 调用方适配 | 更新 | `NewOpenAIService()` 签名变更，所有调用方传入 `tagRepo` |

---

## 5. GenerateZhugeTags 重构伪代码

```go
func (s *OpenAIService) GenerateZhugeTags(script string, flatTags map[string]string, count int) ([]weixin.TagGroup, error) {

    // ─── Step 0: 获取一级分类列表 (level=1) ───
    categories, err := s.tagRepo.GetCategories()
    if err != nil || len(categories) == 0 {
        // 降级：走旧逻辑（全量标签单次调用）
        return s.generateLegacy(script, flatTags, count)
    }
    categoryNames := extractNames(categories) // ["汽车", "科技", "美妆", ...]

    // ─── Step 1: GPT 筛选相关一级分类 ───
    selectedNames, err := s.filterCategories(script, categoryNames, s.cfg.TagCategoryCount)
    if err != nil {
        // 降级：走旧逻辑
        return s.generateLegacy(script, flatTags, count)
    }
    // selectedNames = ["汽车", "科技"]

    // ─── Step 2: 从关联表查询命中分类下的所有二级标签 ───
    selectedIDs := matchCategoryIDs(selectedNames, categories)
    children, _ := s.tagRepo.GetChildrenByCategoryIDs(selectedIDs)

    // 构建分组格式: 【汽车】: 新能源汽车, 燃油车 \n 【科技】: AI, 5G
    groupedTags := buildGroupedTagString(selectedNames, categories, children)

    // ─── Step 3: GPT 从缩小的标签池中精选 ───
    tagGroups, err := s.generateFromFilteredTags(script, groupedTags, flatTags, count)
    return tagGroups, err
}
```

---

## 6. 配置项

| 配置 | 环境变量 | 默认值 | 说明 |
| --- | --- | --- | --- |
| 一级筛选数量 | `TAG_CATEGORY_COUNT` | `5` | Step 1 选择的一级分类数量 |

---

## 7. 降级策略

- 如果 Step 1（一级分类筛选）GPT 调用失败 → **降级为旧逻辑**（全量标签直接调用 Step 2）
- 如果 `GetCategories()` 返回空 → 说明数据库无一级分类数据 → 同样降级为旧逻辑
- 保持向后兼容，不会因重构导致功能中断

---

## 8. 已完成工作

- [x] `cmd/tag-classify/main.go` 脚本开发
- [x] Phase 1 (discover)：GPT-5.2 全量覆盖生成 23 个一级分类
- [x] Phase 2 (assign)：26000+ 标签批量归类（并发 5 worker）
- [x] `model.ZhugeTagCategory` 多对多关联模型
- [x] `zhuge_tag_categories` 关联表创建与数据填充
- [x] 一级分类记录（`level=1`）写入 `zhuge_tags` 表

## 9. 待执行任务

1. **任务 1**：`tag_repo.go` 新增 `GetCategories()` + `GetChildrenByCategoryIDs()`
2. **任务 2**：`openai.go` 新增 `filterCategories()` + `defaultCategoryPrompt`
3. **任务 3**：`openai.go` 重构 `GenerateZhugeTags()` + 更新 `defaultZhugePrompt`
4. **适配注入**：更新 `NewOpenAIService` 签名 + 所有调用方
5. **测试验证**：使用真实文案验证两步筛选效果
