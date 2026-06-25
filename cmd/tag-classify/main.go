package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	openai "github.com/sashabaranov/go-openai"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const (
	modelName       = "gpt-5.2"
	discoverBatch   = 1000 // Phase 1: 每批标签数（全量覆盖）
	assignBatchSize = 300  // Phase 2: 每批归类数
)

// ── Prompts ──

const discoverPrompt = `你是一个兴趣标签分权类专家。请分析以下 %d 个兴趣标签，归纳提炼出 15~25 个一级分类。

要求：
1. 每个分类应该是一个高度概括的领域名称（如"汽车"、"美妆护肤"、"科技数码"）
2. 分类之间不能有明显重叠
3. 每个分类列出 3~5 个代表性标签作为示例
4. 如果有无法归入任何分类的标签，统一归入"其他"

标签列表：
%s

请直接返回 JSON 格式：
[
  {"category": "分类名称", "examples": ["标签1", "标签2", "标签3"]},
  ...
]`

const mergePrompt = `你是一个兴趣标签分类专家。以下是多次分析产出的分类方案，请将它们合并为一套统一的 15~25 个一级分类体系。

要求：
1. 合并含义相近的分类（如"汽车"和"汽车出行"合并为一个）
2. 保留最有代表性的分类名称
3. 确保覆盖所有子标签，不遗漏
4. 每个分类列出 3~5 个代表性标签

多次分析结果：
%s

请直接返回合并后的 JSON 数组：
[
  {"category": "分类名称", "examples": ["标签1", "标签2", "标签3"]},
  ...
]`

const assignPrompt = `将以下兴趣标签归入最合适的一级分类。一个标签可以归入 1~3 个相关分类。

一级分类列表：
%s

标签列表：
%s

请直接返回 JSON 对象，key 为标签名，value 为分类名数组（第一个为主分类）：
{"标签A": ["分类1", "分类2"], "标签B": ["分类1"], ...}

注意：
1. 每个标签至少归入 1 个分类，最多 3 个
2. 数组第一个元素为主分类（最相关的）
3. 如果标签确实不属于任何分类，归入 ["其他"]
4. 只返回 JSON，不要其他内容`

func main() {
	mode := flag.String("mode", "discover", "运行模式: discover(发现分类) / assign(批量归类)")
	categoriesFile := flag.String("categories", "categories.json", "分类体系文件路径 (discover 输出 / assign 输入)")
	dryRun := flag.Bool("dry-run", false, "仅输出不写库 (assign 模式)")
	offset := flag.Int("offset", 0, "断点续传偏移量 (assign 模式)")
	concurrency := flag.Int("concurrency", 5, "并发数 (assign 模式)")
	flag.Parse()

	cfg := config.Load()
	db := mustInitDB(cfg)
	client := mustInitOpenAI(cfg)

	switch *mode {
	case "discover":
		runDiscover(db, client, *categoriesFile)
	case "assign":
		runAssign(db, client, *categoriesFile, *dryRun, *offset, *concurrency)
	default:
		log.Fatalf("未知模式: %s (支持: discover / assign)", *mode)
	}
}

// ── Phase 1: Discover ──

func runDiscover(db *gorm.DB, client *openai.Client, outputFile string) {
	log.Println("═══ Phase 1: 发现分类体系（全量覆盖）═══")

	// 读取所有标签
	var allTags []model.ZhugeTag
	if err := db.Find(&allTags).Error; err != nil {
		log.Fatalf("查询标签失败: %v", err)
	}
	log.Printf("数据库标签总数: %d", len(allTags))

	// 统计分布
	levelCounts := map[int]int{}
	for _, t := range allTags {
		levelCounts[t.WxLevel]++
	}
	log.Printf("标签分布: %v", levelCounts)

	// 提取所有标签名
	names := make([]string, len(allTags))
	for i, t := range allTags {
		names[i] = t.Text
	}

	// 全量分批：每批 discoverBatch 个，确保 100% 覆盖
	totalBatches := (len(names) + discoverBatch - 1) / discoverBatch
	log.Printf("全量分批: %d 批 × %d 个/批", totalBatches, discoverBatch)

	var batchResults []string
	for i := 0; i < len(names); i += discoverBatch {
		end := i + discoverBatch
		if end > len(names) {
			end = len(names)
		}
		batch := names[i:end]
		batchNum := i/discoverBatch + 1
		log.Printf("─── 第 %d/%d 批 (%d 个标签) ───", batchNum, totalBatches, len(batch))

		prompt := fmt.Sprintf(discoverPrompt, len(batch), strings.Join(batch, "、"))
		result, err := callGPT(client, prompt)
		if err != nil {
			log.Printf("第 %d 批失败: %v，跳过", batchNum, err)
			continue
		}
		batchResults = append(batchResults, result)
		log.Printf("第 %d 批完成", batchNum)

		// 短暂延迟避免 rate limit
		time.Sleep(500 * time.Millisecond)
	}

	if len(batchResults) == 0 {
		log.Fatal("所有抽样批次均失败")
	}

	// 合并多批结果
	log.Println("─── 合并分类体系 ───")
	mergeInput := strings.Join(batchResults, "\n\n---\n\n")
	merged, err := callGPT(client, fmt.Sprintf(mergePrompt, mergeInput))
	if err != nil {
		log.Fatalf("合并分类失败: %v", err)
	}

	// 解析并格式化输出
	categories := parseCategories(merged)
	log.Printf("最终分类数: %d", len(categories))
	for _, c := range categories {
		log.Printf("  ✓ %s (%d 示例: %s)", c.Category, len(c.Examples), strings.Join(c.Examples, ", "))
	}

	// 写入文件
	data, _ := json.MarshalIndent(categories, "", "  ")
	if err := os.WriteFile(outputFile, data, 0644); err != nil {
		log.Fatalf("写入文件失败: %v", err)
	}
	log.Println("═══════════════════════════════════════")
	log.Printf("✅ 分类体系已写入: %s", outputFile)
	log.Println("请人工审核后，运行: go run ./cmd/tag-classify --mode=assign")
	log.Println("═══════════════════════════════════════")
}

// ── Phase 2: Assign ──

func runAssign(db *gorm.DB, client *openai.Client, categoriesFile string, dryRun bool, offset int, concurrency int) {
	log.Printf("═══ Phase 2: 批量标签归类 (并发=%d) ═══", concurrency)

	// 读取分类体系
	data, err := os.ReadFile(categoriesFile)
	if err != nil {
		log.Fatalf("读取分类文件失败: %v (请先运行 --mode=discover)", err)
	}
	var categories []CategoryInfo
	if err := json.Unmarshal(data, &categories); err != nil {
		log.Fatalf("解析分类文件失败: %v", err)
	}

	categoryNames := make([]string, len(categories))
	for i, c := range categories {
		categoryNames[i] = c.Category
	}
	log.Printf("加载 %d 个分类: %s", len(categories), strings.Join(categoryNames, ", "))

	// 在 DB 中创建/确保一级分类记录
	categoryIDMap := ensureCategoryRecords(db, categories)

	// 读取需要归类的标签（level=99 或 parent_text 包含"其他"）
	var tags []model.ZhugeTag
	query := db.Where("level = 99 OR (parent_text IS NOT NULL AND parent_text LIKE '%其他%')")
	if err := query.Find(&tags).Error; err != nil {
		log.Fatalf("查询标签失败: %v", err)
	}
	log.Printf("待归类标签数: %d (从 offset=%d 开始)", len(tags), offset)

	if offset >= len(tags) {
		log.Println("✅ 所有标签已处理完毕")
		return
	}
	tags = tags[offset:]

	// 构建批次
	total := len(tags)
	catListStr := strings.Join(categoryNames, "、")

	type batchJob struct {
		num   int
		tags  []model.ZhugeTag
		start int
		end   int
	}

	var jobs []batchJob
	for i := 0; i < total; i += assignBatchSize {
		end := i + assignBatchSize
		if end > total {
			end = total
		}
		jobs = append(jobs, batchJob{
			num:   (offset+i)/assignBatchSize + 1,
			tags:  tags[i:end],
			start: offset + i,
			end:   offset + end - 1,
		})
	}
	log.Printf("共 %d 个批次，并发 %d 个 worker", len(jobs), concurrency)

	// 并发 worker 池
	var successCount, failCount int64
	var wg sync.WaitGroup
	sem := make(chan struct{}, concurrency)

	for _, job := range jobs {
		wg.Add(1)
		sem <- struct{}{} // 占位
		go func(j batchJob) {
			defer wg.Done()
			defer func() { <-sem }() // 释放

			batchNames := make([]string, len(j.tags))
			for k, t := range j.tags {
				batchNames[k] = t.Text
			}

			log.Printf("─── 批次 %d [%d ~ %d / %d] ───", j.num, j.start, j.end, offset+total-1)

			prompt := fmt.Sprintf(assignPrompt, catListStr, strings.Join(batchNames, "、"))
			result, err := callGPT(client, prompt)
			if err != nil {
				log.Printf("批次 %d 失败: %v", j.num, err)
				atomic.AddInt64(&failCount, int64(len(j.tags)))
				return
			}

			assignments := parseAssignments(result)
			if len(assignments) == 0 {
				log.Printf("批次 %d 解析为空，跳过", j.num)
				atomic.AddInt64(&failCount, int64(len(j.tags)))
				return
			}

			for _, t := range j.tags {
				cats, ok := assignments[t.Text]
				if !ok || len(cats) == 0 {
					cats = []string{"其他"}
				}

				primaryCat := cats[0]
				primaryID, exists := categoryIDMap[primaryCat]
				if !exists {
					primaryID = categoryIDMap["其他"]
					primaryCat = "其他"
				}

				if dryRun {
					log.Printf("  [dry-run] %s → %v", t.Text, cats)
				} else {
					if err := db.Model(&model.ZhugeTag{}).Where("id = ?", t.ID).Updates(map[string]any{
						"parent_id":   primaryID,
						"parent_text": primaryCat,
						"wx_level":    2,
					}).Error; err != nil {
						log.Printf("  ✗ 更新失败 %s: %v", t.Text, err)
						atomic.AddInt64(&failCount, 1)
						continue
					}

					for _, cat := range cats {
						catID, ok := categoryIDMap[cat]
						if !ok {
							continue
						}
						rel := model.ZhugeTagCategory{TagID: t.ID, CategoryID: catID}
						db.Where("tag_id = ? AND category_id = ?", t.ID, catID).FirstOrCreate(&rel)
					}
				}
				atomic.AddInt64(&successCount, 1)
			}

			log.Printf("批次 %d 完成: %d 标签已归类", j.num, len(j.tags))
		}(job)
	}

	wg.Wait()

	log.Println("═══════════════════════════════════════")
	if dryRun {
		log.Printf("✅ [dry-run] 完成. 成功=%d, 失败=%d", successCount, failCount)
	} else {
		log.Printf("✅ 归类完成. 成功=%d, 失败=%d", successCount, failCount)
	}
	log.Println("═══════════════════════════════════════")
}

// ── 工具函数 ──

type CategoryInfo struct {
	Category string   `json:"category"`
	Examples []string `json:"examples"`
}

func mustInitDB(cfg *config.Config) *gorm.DB {
	db, err := gorm.Open(mysql.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		log.Fatalf("数据库连接失败: %v", err)
	}
	// 自动创建关联表
	if err := db.AutoMigrate(&model.ZhugeTagCategory{}); err != nil {
		log.Fatalf("迁移 zhuge_tag_categories 表失败: %v", err)
	}
	log.Println("✓ 数据库已连接 (zhuge_tag_categories 表已就绪)")
	return db
}

func mustInitOpenAI(cfg *config.Config) *openai.Client {
	oc := openai.DefaultConfig(cfg.OpenAIAPIKey)
	if cfg.OpenAIBaseURL != "" {
		oc.BaseURL = cfg.OpenAIBaseURL
	}
	log.Printf("✓ OpenAI 客户端就绪 (model=%s, base=%s)", modelName, cfg.OpenAIBaseURL)
	return openai.NewClientWithConfig(oc)
}

func callGPT(client *openai.Client, prompt string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	resp, err := client.CreateChatCompletion(ctx, openai.ChatCompletionRequest{
		Model: modelName,
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: prompt},
		},
	})
	if err != nil {
		return "", fmt.Errorf("GPT 调用失败: %w", err)
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("GPT 返回空")
	}

	raw := resp.Choices[0].Message.Content
	log.Printf("  GPT 响应: tokens(prompt=%d, completion=%d) len=%d",
		resp.Usage.PromptTokens, resp.Usage.CompletionTokens, len(raw))
	return raw, nil
}



func extractJSON(raw string) string {
	s := strings.TrimSpace(raw)
	re := regexp.MustCompile(`(?s)` + "```" + `(?:json)?\s*(.*?)\s*` + "```")
	if m := re.FindStringSubmatch(s); len(m) > 1 {
		s = m[1]
	}
	return strings.TrimSpace(s)
}

func parseCategories(raw string) []CategoryInfo {
	s := extractJSON(raw)
	var cats []CategoryInfo
	if err := json.Unmarshal([]byte(s), &cats); err != nil {
		log.Printf("解析分类 JSON 失败: %v (raw: %.500s)", err, s)
		return nil
	}
	return cats
}

func parseAssignments(raw string) map[string][]string {
	s := extractJSON(raw)
	var m map[string][]string
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		log.Printf("解析归类 JSON 失败: %v (raw: %.500s)", err, s)
		return nil
	}
	return m
}

// ensureCategoryRecords 确保 DB 中有一级分类记录，返回 categoryName → categoryID
func ensureCategoryRecords(db *gorm.DB, categories []CategoryInfo) map[string]string {
	result := make(map[string]string, len(categories))
	for _, c := range categories {
		catID := fmt.Sprintf("cat_%s", strings.ReplaceAll(c.Category, " ", "_"))
		tag := model.ZhugeTag{
			ID:    catID,
			Text:  c.Category,
			WxLevel: 1,
		}
		// Upsert: 存在则跳过
		if err := db.Where("id = ?", catID).FirstOrCreate(&tag).Error; err != nil {
			log.Printf("创建分类 %s 失败: %v", c.Category, err)
			continue
		}
		result[c.Category] = catID
	}
	log.Printf("✓ %d 个一级分类已就绪", len(result))
	return result
}
