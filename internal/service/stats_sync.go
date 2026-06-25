package service

import (
	"context"
	"fmt"
	"log"
	"math"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"golang.org/x/sync/errgroup"
)

var feishuHeaders = []string{
	"采集时间",
	"发布后小时",
	"完播率",
	"平均播放时长",
	"播放量",
	"推荐",
	"喜欢",
	"评论量",
	"分享量",
	"关注量",
	"单小时播放量",
	"推荐率",
	"评论率",
	"关注率",
	"转发率",
}

type StatsSyncService struct {
	authorRepo      *repository.AuthorRepo
	authorVideoRepo *repository.AuthorVideoRepo
	videoStatRepo   *repository.VideoStatRepo
	cursorRepo      *repository.FeishuSyncCursorRepo
	spreadsheetRepo *repository.FeishuSpreadsheetRepo
	folderRepo      *repository.FeishuFolderRepo
	sheetTabRepo    *repository.FeishuSheetTabRepo
	strategyRepo    *repository.StrategyRepo
	feishuClient    *feishu.Client
	matchService    *AuthorVideoMatchService
}

func NewStatsSyncService(
	authorRepo *repository.AuthorRepo,
	authorVideoRepo *repository.AuthorVideoRepo,
	videoStatRepo *repository.VideoStatRepo,
	cursorRepo *repository.FeishuSyncCursorRepo,
	spreadsheetRepo *repository.FeishuSpreadsheetRepo,
	folderRepo *repository.FeishuFolderRepo,
	sheetTabRepo *repository.FeishuSheetTabRepo,
	strategyRepo *repository.StrategyRepo,
	feishuClient *feishu.Client,
	matchService *AuthorVideoMatchService,
) *StatsSyncService {
	return &StatsSyncService{
		authorRepo:      authorRepo,
		authorVideoRepo: authorVideoRepo,
		videoStatRepo:   videoStatRepo,
		cursorRepo:      cursorRepo,
		spreadsheetRepo: spreadsheetRepo,
		folderRepo:      folderRepo,
		sheetTabRepo:    sheetTabRepo,
		strategyRepo:    strategyRepo,
		feishuClient:    feishuClient,
		matchService:    matchService,
	}
}

func (s *StatsSyncService) Run() string {
	// ── 前置步骤：批量回填 author_id ──
	if s.matchService != nil {
		unmatched, err := s.videoStatRepo.GetUnmatchedStats()
		if err != nil {
			log.Printf("service=stats-sync action=get_unmatched error=%v", err)
		} else if len(unmatched) > 0 {
			log.Printf("service=stats-sync action=fill_author_ids unmatched_count=%d", len(unmatched))
			s.matchService.FillAuthorIDs(unmatched)
		}
	}

	// ── 飞书同步 ──
	authors, err := s.authorRepo.ListFeishuEnabled()
	if err != nil {
		log.Printf("service=stats-sync action=list_authors error=%v", err)
		return "error: list authors"
	}

	if len(authors) == 0 {
		return "no feishu-enabled authors"
	}

	log.Printf("service=stats-sync action=run author_count=%d", len(authors))

	var (
		mu          sync.Mutex
		totalSynced int
	)

	g, _ := errgroup.WithContext(context.Background())
	g.SetLimit(5)

	for _, author := range authors {
		vauthor := author
		g.Go(func() error {
			log.Printf("service=stats-sync action=sync_author author=%d", vauthor.ID)
			count, err := s.syncAuthor(vauthor)
			if err != nil {
				log.Printf("service=stats-sync action=sync_author author=%d error=%v", vauthor.ID, err)
				return nil
			}
			mu.Lock()
			totalSynced += count
			mu.Unlock()
			return nil
		})
	}
	g.Wait()

	result := fmt.Sprintf("synced %d records for %d authors", totalSynced, len(authors))
	log.Printf("service=stats-sync action=summary total_synced=%d author_count=%d", totalSynced, len(authors))
	return result
}

func (s *StatsSyncService) syncAuthor(author model.Author) (int, error) {
	cursor, _ := s.cursorRepo.GetByAuthorID(author.ID)
	lastSyncedAt := time.Time{}
	if cursor != nil {
		lastSyncedAt = cursor.LastSyncedAt
	}

	stats, err := s.videoStatRepo.GetUnsyncedByAuthorID(author.ID, lastSyncedAt)
	if err != nil {
		return 0, fmt.Errorf("get unsynced stats: %w", err)
	}
	if len(stats) == 0 {
		return 0, nil
	}

	log.Printf("service=stats-sync action=sync_author author=%d unsynced_count=%d", author.ID, len(stats))

	// ── 预加载：author_videos(create_time) + strategy ──
	avIDs := collectAuthorVideoIDs(stats)
	avMap := s.buildAuthorVideoMap(avIDs)
	strategy, _ := s.strategyRepo.GetByAuthorID(author.ID)

	// ── 确保文件夹层级 ──
	authorFolderToken, err := s.ensureAuthorFolder(author)
	if err != nil {
		return 0, fmt.Errorf("ensure author folder: %w", err)
	}

	// ── 按 export_id 分组（每个视频独立 sheet）──
	videoGroups := groupByExportID(stats)

	for exportID, videoStats := range videoGroups {
		// 确定发布日期（优先 author_videos.create_time）
		publishDate := resolvePublishDate(videoStats, avMap)
		month := publishDate[:7] // "2026-04"

		// 平台（取第一条）
		platform := model.PlatformWeixin
		if videoStats[0].Platform != "" {
			platform = videoStats[0].Platform
		}

		monthFolderToken, err := s.ensureMonthFolder(author.ID, authorFolderToken, month)
		if err != nil {
			log.Printf("service=stats-sync action=ensure_month_folder month=%s error=%v", month, err)
			continue
		}

		platformFolderToken, err := s.ensurePlatformFolder(author.ID, monthFolderToken, month, platform)
		if err != nil {
			log.Printf("service=stats-sync action=ensure_platform_folder month=%s platform=%s error=%v", month, platform, err)
			continue
		}

		spreadsheetToken, err := s.ensureMonthSpreadsheet(author.ID, platformFolderToken, month, platform)
		if err != nil {
			log.Printf("service=stats-sync action=ensure_month_spreadsheet month=%s platform=%s error=%v", month, platform, err)
			continue
		}

		// ── 确保 sheet tab（按 export_id，命名按发布日期+后缀）──
		videoDesc := resolveDescription(videoStats, avMap)
		tab, created, err := s.ensureSheetTab(author.ID, spreadsheetToken, month, publishDate, exportID, videoDesc, strategy)
		if err != nil {
			log.Printf("service=stats-sync action=ensure_sheet_tab export_id=%s error=%v", exportID, err)
			continue
		}
		if created {
			log.Printf("service=stats-sync action=create_sheet_tab export_id=%s publish_date=%s sheet=%s(%s)", exportID, publishDate, tab.SheetTitle, tab.SheetID)
		}

		// ── 构建数据行（含效率指标）──
		rows, metrics := s.buildRowsWithMetrics(videoStats, avMap)

		// ── 追加行到飞书 ──
		updatedRange, err := s.feishuClient.AppendRows(spreadsheetToken, tab.SheetID, rows)
		if err != nil {
			log.Printf("service=stats-sync action=append_rows sheet=%s error=%v", tab.SheetTitle, err)
			continue
		}

		// 新追加行左对齐 + 百分比 formatter
		if updatedRange != "" {
			alignItems := []feishu.CellStyleItem{
				{Ranges: []string{updatedRange}, Style: map[string]interface{}{"hAlign": 0}},
			}
			if err := s.feishuClient.BatchSetCellStyle(spreadsheetToken, alignItems); err != nil {
				log.Printf("service=stats-sync action=align_appended_rows sheet=%s error=%v", tab.SheetTitle, err)
			}
		}

		// ── 阈值着色 ──
		if strategy != nil && updatedRange != "" {
			startRow := parseStartRow(updatedRange)
			if startRow > 0 {
				s.colorizeRows(spreadsheetToken, tab.SheetID, startRow, metrics, strategy)
			}
		}

		// ── 全列 formatter（最后执行，防止被对齐/着色覆盖） ──
		endRow := parseEndRow(updatedRange)
		if endRow > 0 {
			s.applyColumnFormatters(spreadsheetToken, tab.SheetID, endRow)
		}

		// ── 标记已同步 ──
		syncedIDs := make([]int64, len(videoStats))
		for i, st := range videoStats {
			syncedIDs[i] = st.ID
			log.Printf("service=stats-sync action=sync_stat stat=%d export_id=%s publish_date=%s sheet=%s", st.ID, st.ExportID, publishDate, tab.SheetTitle)
		}
		s.videoStatRepo.MarkSynced(syncedIDs)
	}

	lastStat := stats[len(stats)-1]
	cursorSyncCount := len(stats)
	if cursor != nil {
		cursorSyncCount += cursor.SyncCount
	}
	s.cursorRepo.Upsert(&model.FeishuSyncCursor{
		AuthorID:     author.ID,
		LastSyncedAt: lastStat.CreatedAt,
		SyncCount:    cursorSyncCount,
	})

	log.Printf("service=stats-sync action=sync_author author=%d synced=%d", author.ID, len(stats))
	return len(stats), nil
}

// ── 文件夹/表格确保方法 ──

func (s *StatsSyncService) ensureAuthorFolder(author model.Author) (string, error) {
	existing, _ := s.folderRepo.GetAuthorFolder(author.ID)
	if existing != nil {
		return existing.FolderToken, nil
	}

	if author.FeishuFolderToken == nil || *author.FeishuFolderToken == "" {
		return "", fmt.Errorf("author=%d has no feishu_folder_token", author.ID)
	}

	token, err := s.feishuClient.CreateFolder(*author.FeishuFolderToken, author.Nickname)
	if err != nil {
		return "", err
	}

	s.folderRepo.Create(&model.FeishuFolder{
		AuthorID:    author.ID,
		FolderType:  "author",
		FolderToken: token,
		Name:        author.Nickname,
	})

	return token, nil
}

func (s *StatsSyncService) ensureMonthFolder(authorID int64, authorFolderToken, month string) (string, error) {
	existing, _ := s.folderRepo.GetMonthFolder(authorID, month)
	if existing != nil {
		return existing.FolderToken, nil
	}

	name := formatMonthTitle(month)
	token, err := s.feishuClient.CreateFolder(authorFolderToken, name)
	if err != nil {
		return "", err
	}

	s.folderRepo.Create(&model.FeishuFolder{
		AuthorID:    authorID,
		FolderType:  "month",
		Month:       month,
		FolderToken: token,
		Name:        name,
	})

	return token, nil
}

func (s *StatsSyncService) ensurePlatformFolder(authorID int64, monthFolderToken, month, platform string) (string, error) {
	existing, _ := s.folderRepo.GetPlatformFolder(authorID, month, platform)
	if existing != nil {
		return existing.FolderToken, nil
	}

	displayName := model.GetPlatformDisplayName(platform)
	token, err := s.feishuClient.CreateFolder(monthFolderToken, displayName)
	if err != nil {
		return "", err
	}

	s.folderRepo.Create(&model.FeishuFolder{
		AuthorID:    authorID,
		FolderType:  "platform",
		Month:       month,
		Platform:    platform,
		FolderToken: token,
		Name:        displayName,
	})

	return token, nil
}

func (s *StatsSyncService) ensureMonthSpreadsheet(authorID int64, platformFolderToken, month, platform string) (string, error) {
	existing, _ := s.spreadsheetRepo.GetByAuthorMonthPlatform(authorID, month, platform)
	if existing != nil {
		_, err := s.feishuClient.GetSheets(existing.SpreadsheetToken)
		if err == nil {
			return existing.SpreadsheetToken, nil
		}
		log.Printf("service=stats-sync action=ensure_month_spreadsheet spreadsheet=%s result=deleted_from_feishu", existing.SpreadsheetToken)
		s.sheetTabRepo.DeleteBySpreadsheetToken(existing.SpreadsheetToken)
		s.spreadsheetRepo.Delete(existing.ID)
	}

	title := formatMonthTitle(month)
	token, err := s.feishuClient.CreateSpreadsheet(platformFolderToken, title)
	if err != nil {
		return "", err
	}

	s.spreadsheetRepo.Create(&model.FeishuSpreadsheet{
		AuthorID:         authorID,
		Platform:         platform,
		Month:            month,
		SpreadsheetToken: token,
		Title:            title,
	})

	return token, nil
}

// ensureSheetTab 确保视频对应的 sheet tab 存在，返回 (tab, 是否新创建, error)
// 命名规则：同日第1个视频 "04-03"，第2个 "04-03B"，第3个 "04-03C"
func (s *StatsSyncService) ensureSheetTab(
	authorID int64, spreadsheetToken, month, publishDate, exportID, description string,
	strategy *model.AuthorPromoteStrategy,
) (*model.FeishuSheetTab, bool, error) {
	existing, _ := s.sheetTabRepo.GetByExportID(authorID, month, exportID)
	if existing != nil {
		return existing, false, nil
	}

	// 计算同发布日期已有多少个 sheet tab（决定后缀）
	count, _ := s.sheetTabRepo.CountByPublishDate(authorID, month, publishDate)
	dateStr := publishDate[8:] // "03"
	sheetTitle := dateStr
	if count > 0 {
		// 第2个用B，第3个用C...
		suffix := string(rune('A' + count))
		sheetTitle = dateStr + suffix
	}

	feishuSheets, _ := s.feishuClient.GetSheets(spreadsheetToken)
	sheetCount := len(feishuSheets)

	// 计算插入位置（按标题排序，保持日期顺序）
	insertIndex := sheetCount
	for i, existingSheet := range feishuSheets {
		if sheetTitle < existingSheet.Title {
			insertIndex = i
			break
		}
	}

	log.Printf("service=stats-sync action=ensure_sheet_tab author=%d spreadsheet=%s sheet=%s insert_index=%d",
		authorID, spreadsheetToken, sheetTitle, insertIndex)

	sheetID, err := s.feishuClient.CreateSheet(spreadsheetToken, sheetTitle, insertIndex)
	if err != nil {
		log.Printf("service=stats-sync action=create_sheet error=%v", err)
		// 飞书已有同名 tab 但 DB 缺记录（数据不一致），尝试复用已有 sheet
		if strings.Contains(err.Error(), "sheetTitle already exist") {
			for _, fs := range feishuSheets {
				if fs.Title == sheetTitle {
					sheetID = fs.SheetID
					break
				}
			}
		}
		if sheetID == "" {
			return nil, false, fmt.Errorf("create sheet %s: %w", sheetTitle, err)
		}
		// 复用已有 sheet：仅补录 DB 记录，跳过标题/表头/参考值初始化
		log.Printf("service=stats-sync action=reuse_existing_sheet sheet=%s(%s)", sheetTitle, sheetID)
		reusedTab := &model.FeishuSheetTab{
			AuthorID:         authorID,
			Month:            month,
			PublishDate:      publishDate,
			ExportID:         exportID,
			SpreadsheetToken: spreadsheetToken,
			SheetID:          sheetID,
			SheetTitle:       sheetTitle,
		}
		if err := s.sheetTabRepo.Create(reusedTab); err != nil {
			log.Printf("service=stats-sync action=save_reused_sheet_tab error=%v", err)
		}
		return reusedTab, false, nil
	}

	// 第1行：写入视频描述（合并单元格 + 左对齐）
	if description != "" {
		if err := s.feishuClient.WriteTitleRow(spreadsheetToken, sheetID, description, len(feishuHeaders)); err != nil {
			log.Printf("service=stats-sync action=write_title sheet=%s error=%v", sheetTitle, err)
		}
		titleRange := fmt.Sprintf("%s!A1:%s1", sheetID, string(rune('A'+len(feishuHeaders)-1)))
		titleStyle := []feishu.CellStyleItem{
			{Ranges: []string{titleRange}, Style: map[string]interface{}{"hAlign": 0}},
		}
		if err := s.feishuClient.BatchSetCellStyle(spreadsheetToken, titleStyle); err != nil {
			log.Printf("service=stats-sync action=style_title_align sheet=%s error=%v", sheetTitle, err)
		}
	}

	// 第2行：写入表头
	if err := s.feishuClient.WriteHeader(spreadsheetToken, sheetID, feishuHeaders); err != nil {
		log.Printf("service=stats-sync action=write_header sheet=%s error=%v", sheetTitle, err)
	}

	// 第3行：写入参考值（投放策略阈值）
	s.writeReferenceRow(spreadsheetToken, sheetID, strategy)

	// 全列左对齐 + formatter（新建 tab 只有3行：标题+表头+参考值）
	alignRange := fmt.Sprintf("%s!A1:%s3", sheetID, string(rune('A'+len(feishuHeaders)-1)))
	alignStyles := []feishu.CellStyleItem{
		{Ranges: []string{alignRange}, Style: map[string]interface{}{"hAlign": 0}},
	}
	if err := s.feishuClient.BatchSetCellStyle(spreadsheetToken, alignStyles); err != nil {
		log.Printf("service=stats-sync action=set_align_style sheet=%s error=%v", sheetTitle, err)
	}
	s.applyColumnFormatters(spreadsheetToken, sheetID, 3)

	// 删除默认 Sheet1（如果存在且没有其他 tab 记录）
	existingTabs, _ := s.sheetTabRepo.ListByAuthorMonth(authorID, month)
	if len(existingTabs) == 0 {
		for _, ds := range feishuSheets {
			if ds.Title == "Sheet1" || ds.Title == "工作表1" {
				if err := s.feishuClient.DeleteSheet(spreadsheetToken, ds.SheetID); err != nil {
					log.Printf("service=stats-sync action=delete_default_sheet error=%v", err)
				} else {
					log.Printf("service=stats-sync action=delete_default_sheet sheet=%s result=success", ds.Title)
				}
			}
		}
	}

	newTab := &model.FeishuSheetTab{
		AuthorID:         authorID,
		Month:            month,
		PublishDate:      publishDate,
		ExportID:         exportID,
		SpreadsheetToken: spreadsheetToken,
		SheetID:          sheetID,
		SheetTitle:       sheetTitle,
	}
	if err := s.sheetTabRepo.Create(newTab); err != nil {
		log.Printf("service=stats-sync action=save_sheet_tab error=%v", err)
	}
	return newTab, true, nil
}

// ── 效率指标 ──

type efficiencyMetrics struct {
	HourlyPlays   *float64
	RecommendRate *float64
	CommentRate   *float64
	FollowRate    *float64
	ShareRate     *float64
}

func (m efficiencyMetrics) Get(key string) *float64 {
	switch key {
	case "hourly_plays":
		return m.HourlyPlays
	case "recommend_rate":
		return m.RecommendRate
	case "comment_rate":
		return m.CommentRate
	case "follow_rate":
		return m.FollowRate
	case "share_rate":
		return m.ShareRate
	}
	return nil
}

// writeReferenceRow 在第3行写入投放策略阈值参考值
func (s *StatsSyncService) writeReferenceRow(spreadsheetToken, sheetID string, strategy *model.AuthorPromoteStrategy) {
	if strategy == nil {
		return
	}

	// 构建参考值行（与 feishuHeaders 对齐，共22列）
	row := make([]interface{}, len(feishuHeaders))
	for i := range row {
		row[i] = ""
	}
	row[0] = "参考值" // 采集时间列

	// 效率指标列（index 10-14）
	row[10] = strategy.HourlyPlayThreshold        // 单小时播放量
	row[11] = strategy.LikeRateThreshold / 100    // 推荐率
	row[12] = strategy.CommentRateThreshold / 100 // 评论率
	row[13] = strategy.FollowRateThreshold / 100  // 关注率
	row[14] = strategy.ShareRateThreshold / 100   // 转发率

	if err := s.feishuClient.WriteRow(spreadsheetToken, sheetID, 3, row); err != nil {
		log.Printf("service=stats-sync action=write_reference_row error=%v", err)
		return
	}

	// 参考值行全行红色背景
	refRange := fmt.Sprintf("%s!A3:%s3", sheetID, string(rune('A'+len(feishuHeaders)-1)))
	refPctC := fmt.Sprintf("%s!C3:C3", sheetID)
	refPctLO := fmt.Sprintf("%s!L3:O3", sheetID)
	style := []feishu.CellStyleItem{
		// 全行红底（非百分比列）
		{Ranges: []string{refRange}, Style: map[string]interface{}{"backColor": "#FF0000", "foreColor": "#FFFFFF", "hAlign": 0}},
		// 百分比列：红底 + formatter（后者覆盖前者，保留 formatter）
		{Ranges: []string{refPctC, refPctLO}, Style: map[string]interface{}{"backColor": "#FF0000", "foreColor": "#FFFFFF", "hAlign": 0, "formatter": "0.00%"}},
	}
	if err := s.feishuClient.BatchSetCellStyle(spreadsheetToken, style); err != nil {
		log.Printf("service=stats-sync action=style_reference_row error=%v", err)
	}
}

func (s *StatsSyncService) buildRowsWithMetrics(stats []model.VideoStat, avMap map[int64]*model.AuthorVideo) ([][]interface{}, []efficiencyMetrics) {
	rows := make([][]interface{}, len(stats))
	metrics := make([]efficiencyMetrics, len(stats))

	for i, st := range stats {
		// ── 发布后小时 ──
		var hoursSincePublish interface{} = ""
		av := avMap[st.AuthorVideoID]
		if av == nil {
			log.Printf("service=stats-sync action=hours_debug stat_id=%d author_video_id=%d av=nil", st.ID, st.AuthorVideoID)
		}
		var publishTime time.Time
		if av != nil && av.GetCreateTime() != nil {
			publishTime = *av.GetCreateTime()
			collectTime := parseCollectTime(st.CollectDate)
			if !collectTime.IsZero() && !publishTime.IsZero() {
				hours := collectTime.Sub(publishTime).Hours()
				hoursSincePublish = math.Round(hours*10) / 10 // 保留 1 位小数
			}
		} else if av != nil {
			log.Printf("service=stats-sync action=hours_debug stat_id=%d author_video_id=%d create_time=nil raw_data_len=%d", st.ID, st.AuthorVideoID, len(av.RawData))
		}

		// ── 效率指标（需要前一条记录）──
		var hourlyPlays, recommendRate, commentRate, followRate, shareRate interface{} = "", "", "", "", ""
		prevStat, _ := s.videoStatRepo.GetPreviousStat(st.ExportID, st.ID)
		if prevStat != nil {
			deltaPlay := st.PlayCount - prevStat.PlayCount
			if deltaPlay > 0 {
				prevTime := parseCollectTime(prevStat.CollectDate)
				currTime := parseCollectTime(st.CollectDate)
				if !prevTime.IsZero() && !currTime.IsZero() {
					deltaHours := currTime.Sub(prevTime).Hours()
					if deltaHours > 0 {
						hp := float64(deltaPlay) / deltaHours
						hourlyPlays = int(math.Round(hp))
						metrics[i].HourlyPlays = &hp
					}
				}

				rr := float64(st.RecommendCount-prevStat.RecommendCount) / float64(deltaPlay)
				recommendRate = rr
				metrics[i].RecommendRate = &rr

				cr := float64(st.CommentCount-prevStat.CommentCount) / float64(deltaPlay)
				commentRate = cr
				metrics[i].CommentRate = &cr

				fr := float64(st.FollowCount-prevStat.FollowCount) / float64(deltaPlay)
				followRate = fr
				metrics[i].FollowRate = &fr

				sr := float64(st.ShareCount-prevStat.ShareCount) / float64(deltaPlay)
				shareRate = sr
				metrics[i].ShareRate = &sr
			} else {
				hourlyPlays = "-"
				recommendRate = "-"
				commentRate = "-"
				followRate = "-"
				shareRate = "-"
			}
		}

		rows[i] = []interface{}{
			st.CollectDate,
			hoursSincePublish,
			parsePercent(st.CompletionRate),
			parseDurationSeconds(st.AvgPlayDuration),
			st.PlayCount,
			st.RecommendCount,
			st.LikeCount,
			st.CommentCount,
			st.ShareCount,
			st.FollowCount,
			hourlyPlays,
			recommendRate,
			commentRate,
			followRate,
			shareRate,
		}
	}
	return rows, metrics
}

// ── 阈值着色 ──

func (s *StatsSyncService) colorizeRows(
	spreadsheetToken, sheetID string,
	startRow int,
	metrics []efficiencyMetrics,
	strategy *model.AuthorPromoteStrategy,
) {
	if strategy == nil {
		return
	}

	var items []feishu.CellStyleItem
	// 颜色样式（按列区分 formatter，L-O 百分比列需保留 formatter）
	orangeNum := map[string]interface{}{"backColor": "#FF9900", "hAlign": 0}
	greenNum := map[string]interface{}{"backColor": "#00CC66", "hAlign": 0}
	orangePct := map[string]interface{}{"backColor": "#FF9900", "hAlign": 0, "formatter": "0.00%"}
	greenPct := map[string]interface{}{"backColor": "#00CC66", "hAlign": 0, "formatter": "0.00%"}

	colMap := map[string]string{
		"hourly_plays":   "K",
		"recommend_rate": "L",
		"comment_rate":   "M",
		"follow_rate":    "N",
		"share_rate":     "O",
	}

	// 百分比列集合
	pctCols := map[string]bool{"L": true, "M": true, "N": true, "O": true}

	thresholds := map[string]float64{
		"hourly_plays":   float64(strategy.HourlyPlayThreshold),
		"recommend_rate": strategy.LikeRateThreshold / 100,
		"comment_rate":   strategy.CommentRateThreshold / 100,
		"follow_rate":    strategy.FollowRateThreshold / 100,
		"share_rate":     strategy.ShareRateThreshold / 100,
	}

	for i, m := range metrics {
		row := startRow + i
		for key, col := range colMap {
			val := m.Get(key)
			threshold := thresholds[key]
			if val == nil || threshold <= 0 {
				continue
			}

			var color map[string]interface{}
			if pctCols[col] {
				color = greenPct
				if *val >= threshold {
					color = orangePct
				}
			} else {
				color = greenNum
				if *val >= threshold {
					color = orangeNum
				}
			}
			items = append(items, feishu.CellStyleItem{
				Ranges: []string{fmt.Sprintf("%s!%s%d:%s%d", sheetID, col, row, col, row)},
				Style:  color,
			})
		}
	}

	if len(items) > 0 {
		if err := s.feishuClient.BatchSetCellStyle(spreadsheetToken, items); err != nil {
			log.Printf("service=stats-sync action=colorize error=%v items=%d", err, len(items))
		} else {
			log.Printf("service=stats-sync action=colorize items=%d", len(items))
		}
	}
}

// ── 辅助函数 ──

// applyColumnFormatters 统一设置全列 formatter（最后调用，确保不被其他样式覆盖）
func (s *StatsSyncService) applyColumnFormatters(spreadsheetToken, sheetID string, maxRow int) {
	r := strconv.Itoa(maxRow)
	fmtStyles := []feishu.CellStyleItem{
		{Ranges: []string{fmt.Sprintf("%s!A1:A%s", sheetID, r)}, Style: map[string]interface{}{"hAlign": 0, "formatter": "@"}},        // 采集时间：纯文本
		{Ranges: []string{fmt.Sprintf("%s!B1:B%s", sheetID, r)}, Style: map[string]interface{}{"hAlign": 0, "formatter": "#,##0.00"}}, // 发布后小时：千分位小数
		{Ranges: []string{fmt.Sprintf("%s!C1:C%s", sheetID, r)}, Style: map[string]interface{}{"hAlign": 0, "formatter": "0.00%"}},    // 完播率：百分比
		{Ranges: []string{fmt.Sprintf("%s!D1:D%s", sheetID, r)}, Style: map[string]interface{}{"hAlign": 0, "formatter": "#,##0"}},    // 平均播放时长：数字
		{Ranges: []string{fmt.Sprintf("%s!E1:K%s", sheetID, r)}, Style: map[string]interface{}{"hAlign": 0, "formatter": "#,##0"}},    // 播放量~单小时播放量：千分位
		{Ranges: []string{fmt.Sprintf("%s!L1:O%s", sheetID, r)}, Style: map[string]interface{}{"hAlign": 0, "formatter": "0.00%"}},    // 推荐率~转发率：百分比
	}
	if err := s.feishuClient.BatchSetCellStyle(spreadsheetToken, fmtStyles); err != nil {
		log.Printf("service=stats-sync action=apply_column_formatters sheet=%s error=%v", sheetID, err)
	}
}

func collectAuthorVideoIDs(stats []model.VideoStat) []int64 {
	seen := make(map[int64]bool)
	var ids []int64
	for _, s := range stats {
		if s.AuthorVideoID > 0 && !seen[s.AuthorVideoID] {
			seen[s.AuthorVideoID] = true
			ids = append(ids, s.AuthorVideoID)
		}
	}
	return ids
}

func (s *StatsSyncService) buildAuthorVideoMap(ids []int64) map[int64]*model.AuthorVideo {
	avMap := make(map[int64]*model.AuthorVideo)
	if len(ids) == 0 {
		return avMap
	}
	avList, err := s.authorVideoRepo.GetByIDs(ids)
	if err != nil {
		log.Printf("service=stats-sync action=build_av_map error=%v", err)
		return avMap
	}
	for i := range avList {
		avMap[avList[i].ID] = &avList[i]
	}
	return avMap
}

// groupByExportID 按视频 export_id 分组（每个视频独立一个 sheet）
func groupByExportID(stats []model.VideoStat) map[string][]model.VideoStat {
	grouped := make(map[string][]model.VideoStat)
	for _, s := range stats {
		grouped[s.ExportID] = append(grouped[s.ExportID], s)
	}
	return grouped
}

// resolvePublishDate 确定视频的发布日期（优先 author_videos.create_time，降级到 publish_date / collect_date）
func resolvePublishDate(stats []model.VideoStat, avMap map[int64]*model.AuthorVideo) string {
	// 优先取 author_videos.raw_data.createTime
	if len(stats) > 0 {
		if av, ok := avMap[stats[0].AuthorVideoID]; ok && av.GetCreateTime() != nil {
			return av.GetCreateTime().Format("2006-01-02")
		}
	}
	// 降级到 video_stats.publish_date
	if len(stats) > 0 && len(stats[0].PublishDate) >= 10 {
		return stats[0].PublishDate[:10]
	}
	// 再降级到 collect_date
	if len(stats) > 0 && len(stats[0].CollectDate) >= 10 {
		return stats[0].CollectDate[:10]
	}
	return time.Now().Format("2006-01-02")
}

// resolveDescription 获取视频描述（优先 author_videos.description，降级到 video_stats.description）
func resolveDescription(stats []model.VideoStat, avMap map[int64]*model.AuthorVideo) string {
	if len(stats) > 0 {
		if av, ok := avMap[stats[0].AuthorVideoID]; ok && av.Description != "" {
			return av.Description
		}
	}
	if len(stats) > 0 && stats[0].Description != "" {
		return stats[0].Description
	}
	if len(stats) > 0 {
		return stats[0].ExportID
	}
	return ""
}

func parseCollectTime(s string) time.Time {
	s = strings.TrimSpace(s)
	layouts := []string{
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

// parsePercent 将百分比字符串转为小数 ("18.94%" → 0.1894)
func parsePercent(s string) interface{} {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v / 100
	}
	return s
}

func parseDurationSeconds(s string) interface{} {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 2:
		m, _ := strconv.Atoi(parts[0])
		sec, _ := strconv.Atoi(parts[1])
		return m*60 + sec
	case 3:
		h, _ := strconv.Atoi(parts[0])
		m, _ := strconv.Atoi(parts[1])
		sec, _ := strconv.Atoi(parts[2])
		return h*3600 + m*60 + sec
	}
	return s
}

// parseStartRow 从飞书 AppendRows 返回的 updatedRange 解析起始行号
// 格式如 "sheetID!A5:V8" → 5
var rangeRowRe = regexp.MustCompile(`![A-Z]+(\d+):`)

func parseStartRow(updatedRange string) int {
	matches := rangeRowRe.FindStringSubmatch(updatedRange)
	if len(matches) < 2 {
		return 0
	}
	row, _ := strconv.Atoi(matches[1])
	return row
}

var rangeEndRowRe = regexp.MustCompile(`[A-Z]+(\d+)$`)

func parseEndRow(updatedRange string) int {
	matches := rangeEndRowRe.FindStringSubmatch(updatedRange)
	if len(matches) < 2 {
		return 0
	}
	row, _ := strconv.Atoi(matches[1])
	return row
}

func formatMonthTitle(month string) string {
	parts := strings.Split(month, "-")
	if len(parts) != 2 {
		return month
	}
	return fmt.Sprintf("%s年%s月", parts[0], parts[1])
}
