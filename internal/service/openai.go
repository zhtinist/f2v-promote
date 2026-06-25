package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	openai "github.com/sashabaranov/go-openai"
)

// defaultZhugePrompt 与 Python 完全一致的提示词
const defaultZhugePrompt = `你是一个视频号加热投放专家。根据以下视频口播文案，生成 {count} 组差异化的投放定向标签。

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

请直接返回 JSON 数组：`

// defaultCategoryPrompt 一级分类筛选提示词
const defaultCategoryPrompt = `你是一个视频号投放定向专家。根据以下视频口播文案，从平台兴趣标签的一级分类中选出最相关的 {count} 个分类。

平台一级分类列表：
{categories}

要求：
1. 选择与文案内容最相关的 {count} 个一级分类
2. 优先选择与视频主题直接相关的分类
3. 可以适当选择潜在受众感兴趣的关联分类
4. 只返回 JSON 数组，包含选中分类的名称

口播文案：
{script}

请直接返回 JSON 字符串数组，如 ["汽车", "科技", "财经"]：`

// AITagGroup AI 返回的原始标签组
type AITagGroup struct {
	Name        string `json:"name"`
	InterestTag string `json:"interest_tag"`
	Ages        []int  `json:"ages"`
	Sex         *int   `json:"sex"`
	CityIds     []any  `json:"cityIds"`
}

type OpenAIService struct {
	cfg        *config.Config
	promptRepo *repository.PromptRepo
	tagRepo    *repository.ZhugeTagRepo
	client     *openai.Client
}

func NewOpenAIService(cfg *config.Config, promptRepo *repository.PromptRepo, tagRepo *repository.ZhugeTagRepo) *OpenAIService {
	oc := openai.DefaultConfig(cfg.OpenAIAPIKey)
	if cfg.OpenAIBaseURL != "" {
		oc.BaseURL = cfg.OpenAIBaseURL
	}
	client := openai.NewClientWithConfig(oc)
	return &OpenAIService{
		cfg:        cfg,
		promptRepo: promptRepo,
		tagRepo:    tagRepo,
		client:     client,
	}
}

func (s *OpenAIService) getZhugePrompt() string {
	p, err := s.promptRepo.Get("zhuge_tags")
	if err != nil {
		log.Printf("service=openai action=get_prompt error=%v", err)
		return defaultZhugePrompt
	}
	if p != nil {
		return p.Content
	}

	desc := "生成诸葛智投投放标签组的提示词。支持变量: {count} {script} {available_tags}"
	_, err = s.promptRepo.Upsert("zhuge_tags", "诸葛智投标签生成", defaultZhugePrompt, &desc)
	if err != nil {
		log.Printf("service=openai action=create_default_prompt error=%v", err)
	}
	return defaultZhugePrompt
}

func (s *OpenAIService) getCategoryPrompt() string {
	p, err := s.promptRepo.Get("zhuge_categories")
	if err != nil {
		log.Printf("service=openai action=get_category_prompt error=%v", err)
		return defaultCategoryPrompt
	}
	if p != nil {
		return p.Content
	}

	desc := "一级分类筛选提示词。支持变量: {count} {categories} {script}"
	_, err = s.promptRepo.Upsert("zhuge_categories", "诸葛一级分类筛选", defaultCategoryPrompt, &desc)
	if err != nil {
		log.Printf("service=openai action=create_category_prompt error=%v", err)
	}
	return defaultCategoryPrompt
}

// GenerateZhugeTags 两步筛选：Step1 GPT选分类 → Step2 GPT从缩小标签池精选
func (s *OpenAIService) GenerateZhugeTags(script string, flatTags map[string]model.TagMeta, count int) ([]weixin.TagGroup, error) {
	// ─── Step 0: 获取一级分类列表 ───
	categories, err := s.tagRepo.GetCategories()
	if err != nil || len(categories) == 0 {
		log.Printf("service=openai action=get_categories fallback=legacy error=%v count=%d", err, len(categories))
		return s.generateLegacy(script, flatTags, count)
	}

	categoryNames := make([]string, len(categories))
	for i, c := range categories {
		categoryNames[i] = c.Text
	}
	log.Printf("service=openai action=step0 categories=%d names=%s", len(categories), strings.Join(categoryNames, ", "))

	// ─── Step 1: GPT 筛选相关一级分类 ───
	selectedNames, err := s.filterCategories(script, categoryNames)
	if err != nil {
		log.Printf("service=openai action=filter_categories fallback=legacy error=%v", err)
		return s.generateLegacy(script, flatTags, count)
	}
	log.Printf("service=openai action=step1 selected=%v", selectedNames)

	// ─── Step 2: 从关联表查询命中分类下的所有二级标签 ───
	selectedIDs := matchCategoryIDs(selectedNames, categories)
	children, err := s.tagRepo.GetChildrenByCategoryIDs(selectedIDs)
	if err != nil || len(children) == 0 {
		log.Printf("service=openai action=get_children fallback=legacy error=%v count=%d", err, len(children))
		return s.generateLegacy(script, flatTags, count)
	}
	log.Printf("service=openai action=step2 children=%d from_categories=%d", len(children), len(selectedIDs))

	// 构建分组格式: 【汽车】: 新能源汽车, 燃油车 \n 【科技】: AI, 5G
	groupedTags := buildGroupedTagString(selectedNames, categories, children)

	// ─── Step 3: GPT 从缩小的标签池中精选 ───
	return s.generateFromTags(script, groupedTags, flatTags, count)
}

// filterCategories Step 1: GPT 从一级分类中筛选相关分类
func (s *OpenAIService) filterCategories(script string, categoryNames []string) ([]string, error) {
	prompt := s.getCategoryPrompt()

	filled := strings.ReplaceAll(prompt, "{count}", fmt.Sprintf("%d", s.cfg.TagCategoryCount))
	filled = strings.ReplaceAll(filled, "{categories}", strings.Join(categoryNames, "、"))
	filled = strings.ReplaceAll(filled, "{script}", script)

	log.Printf("service=openai action=filter_categories request={prompt_len=%d category_count=%d}", len(filled), len(categoryNames))

	resp, err := s.callGPT(filled)
	if err != nil {
		return nil, err
	}

	// 解析 JSON 数组
	raw := extractJSONString(resp)
	var selected []string
	if err := json.Unmarshal([]byte(raw), &selected); err != nil {
		return nil, fmt.Errorf("parse category response: %w (raw: %.300s)", err, raw)
	}

	return selected, nil
}

// matchCategoryIDs 将分类名匹配为分类 ID
func matchCategoryIDs(selectedNames []string, categories []model.ZhugeTag) []string {
	nameToID := make(map[string]string, len(categories))
	for _, c := range categories {
		nameToID[c.Text] = c.ID
	}

	var ids []string
	for _, name := range selectedNames {
		if id, ok := nameToID[name]; ok {
			ids = append(ids, id)
		}
	}
	return ids
}

// buildGroupedTagString 按分类分组构建标签字符串
func buildGroupedTagString(selectedNames []string, categories []model.ZhugeTag, children []model.ZhugeTag) string {
	// 构建 categoryID → categoryName
	idToName := make(map[string]string, len(categories))
	for _, c := range categories {
		idToName[c.ID] = c.Text
	}

	// 按 parent_text 分组（主分类）
	grouped := make(map[string][]string)
	for _, t := range children {
		catName := ""
		if t.ParentText != nil {
			catName = *t.ParentText
		}
		if catName == "" {
			catName = "其他"
		}
		grouped[catName] = append(grouped[catName], t.Text)
	}

	// 按 selectedNames 顺序输出
	var lines []string
	for _, name := range selectedNames {
		if tags, ok := grouped[name]; ok {
			lines = append(lines, fmt.Sprintf("【%s】: %s", name, strings.Join(tags, ", ")))
		}
	}
	// 追加未在 selectedNames 中但有数据的分类（交叉分类标签）
	for name, tags := range grouped {
		found := false
		for _, sn := range selectedNames {
			if sn == name {
				found = true
				break
			}
		}
		if !found {
			lines = append(lines, fmt.Sprintf("【%s】: %s", name, strings.Join(tags, ", ")))
		}
	}

	return strings.Join(lines, "\n")
}

// generateFromTags Step 3: 从筛选后的标签池生成标签组
func (s *OpenAIService) generateFromTags(script, availableTags string, flatTags map[string]model.TagMeta, count int) ([]weixin.TagGroup, error) {
	prompt := s.getZhugePrompt()

	filled := strings.ReplaceAll(prompt, "{count}", fmt.Sprintf("%d", count))
	filled = strings.ReplaceAll(filled, "{script}", script)
	filled = strings.ReplaceAll(filled, "{available_tags}", availableTags)

	log.Printf("service=openai action=generate_from_filtered request={model=%s prompt_len=%d tag_pool_len=%d count=%d script=%.100s}",
		s.cfg.OpenAIModel, len(filled), len(availableTags), count, script)

	return s.callAndParseTags(filled, flatTags, count)
}

// generateLegacy 降级：旧逻辑，全量标签单次调用
func (s *OpenAIService) generateLegacy(script string, flatTags map[string]model.TagMeta, count int) ([]weixin.TagGroup, error) {
	prompt := s.getZhugePrompt()

	tagNames := make([]string, 0, len(flatTags))
	for name := range flatTags {
		tagNames = append(tagNames, name)
	}
	if len(tagNames) > 500 {
		tagNames = tagNames[:500]
	}
	availableTags := strings.Join(tagNames, ", ")

	filled := strings.ReplaceAll(prompt, "{count}", fmt.Sprintf("%d", count))
	filled = strings.ReplaceAll(filled, "{script}", script)
	filled = strings.ReplaceAll(filled, "{available_tags}", availableTags)

	log.Printf("service=openai action=generate_legacy request={model=%s prompt_len=%d tag_count=%d count=%d script=%.100s}",
		s.cfg.OpenAIModel, len(filled), len(tagNames), count, script)

	return s.callAndParseTags(filled, flatTags, count)
}

// callAndParseTags 通用：调用 GPT 并解析标签组结果
func (s *OpenAIService) callAndParseTags(filled string, flatTags map[string]model.TagMeta, count int) ([]weixin.TagGroup, error) {
	var lastErr error
	for attempt := 0; attempt < s.cfg.OpenAIMaxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(s.cfg.OpenAIRetryDelay) * time.Second)
		}

		log.Printf("service=openai action=generate_tags attempt=%d prompt_len=%d", attempt+1, len(filled))

		raw, err := s.callGPT(filled)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			log.Printf("service=openai action=generate_tags attempt=%d error=%v", attempt+1, err)
			continue
		}

		aiGroups, err := parseAITagGroups(raw)
		if err != nil {
			lastErr = fmt.Errorf("attempt %d: %w", attempt+1, err)
			log.Printf("service=openai action=generate_tags attempt=%d error=%v", attempt+1, err)
			continue
		}

		result := mapToZhugeFormat(aiGroups, flatTags)
		if len(result) > count {
			result = result[:count]
		}

		log.Printf("service=openai action=generate_tags result=success generated=%d requested=%d", len(result), count)
		return result, nil
	}

	return nil, fmt.Errorf("openai: all %d attempts failed: %w", s.cfg.OpenAIMaxRetries, lastErr)
}

// callGPT 单次 GPT 调用
func (s *OpenAIService) callGPT(prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := s.client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: s.cfg.OpenAIModel,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("GPT call: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("GPT returned no choices")
	}

	raw := resp.Choices[0].Message.Content
	log.Printf("service=openai action=gpt_call response={prompt_tokens=%d completion_tokens=%d content_len=%d}",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, len(raw))
	return raw, nil
}

// extractJSONString 去掉 markdown code block
func extractJSONString(raw string) string {
	s := strings.TrimSpace(raw)
	re := regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")
	if matches := re.FindStringSubmatch(s); len(matches) > 1 {
		s = matches[1]
	}
	return strings.TrimSpace(s)
}

// parseAITagGroups 解析 AI 返回的 JSON
func parseAITagGroups(raw string) ([]AITagGroup, error) {
	s := extractJSONString(raw)

	var groups []AITagGroup
	if err := json.Unmarshal([]byte(s), &groups); err != nil {
		return nil, fmt.Errorf("parse JSON: %w (raw: %.300s)", err, s)
	}
	return groups, nil
}

// mapToZhugeFormat 将 AI 返回的标签名映射为平台 ID
func mapToZhugeFormat(aiGroups []AITagGroup, flatTags map[string]model.TagMeta) []weixin.TagGroup {
	result := make([]weixin.TagGroup, 0, len(aiGroups))

	for _, g := range aiGroups {
		tagName := g.InterestTag
		var matched model.TagMeta

		// 精确匹配
		if meta, ok := flatTags[tagName]; ok {
			matched = meta
		} else {
			// 模糊匹配：子串包含
			for name, meta := range flatTags {
				if strings.Contains(name, tagName) || strings.Contains(tagName, name) {
					matched = meta
					tagName = name
					break
				}
			}
		}

		var interestTagList []weixin.InterestTagRef
		if matched.ID != "" {
			interestTagList = []weixin.InterestTagRef{
				{InterestTag: matched.ID, TagLevel: matched.WxLevel},
			}
		}

		ages := g.Ages
		if len(ages) == 0 {
			ages = []int{1, 2, 3, 4, 5}
		}

		cityIDs := make([]string, 0)
		for _, c := range g.CityIds {
			if s, ok := c.(string); ok {
				cityIDs = append(cityIDs, s)
			}
		}

		result = append(result, weixin.TagGroup{
			Name:            g.Name,
			InterestTagList: interestTagList,
			Ages:            ages,
			Sex:             g.Sex,
			CityIDs:         cityIDs,
		})
	}

	return result
}
