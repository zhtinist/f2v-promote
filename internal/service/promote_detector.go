package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"sync"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"golang.org/x/sync/errgroup"
	"gorm.io/datatypes"
)

const (
	promoteExecTimeout   = 58 * time.Second // 单次执行超时（FC 每分钟触发，预留 2s）
	promotePollInterval  = 15 * time.Second // 轮询间隔
	maxConcurrency       = 5                // 最大并发作者数（防止打爆诸葛 API）
	defaultVideoCooldown = 1 * time.Minute  // 同视频投放冷却时间（可通过配置覆盖）
)

// PromoteDecision 单个视频的投放决策结果
type PromoteDecision struct {
	AuthorVideoID  int64
	AuthorID       int64  // 作者 ID（用于查飞书 sheet tab）
	ExportID       string // 仅用于日志打印
	AuthorName     string // 作者名称
	Description    string // 视频描述
	PromoteType    string // "like_rate" 或 "followers"
	StatIDCurrent  int64
	StatIDPrevious *int64
	StatRawData    []byte // 前后两条 video_stat JSON
	HourlyPlay     int
	LikeRate       float64
	ShareRate      float64

	// 前后数据对比（用于飞书卡片展示）
	CurrentDate    string
	CurrentPlay    int64
	CurrentLike    int64
	CurrentShare   int64
	CurrentFollow  int64
	CurrentComment int64
	PrevDate       string
	PrevPlay       int64
	PrevLike       int64
	PrevShare      int64
	PrevFollow     int64
	PrevComment    int64

	VideoRawData  []byte
	AuthorRawData []byte
}

// PromoteDetectorService 检测服务（Promote Cron 使用）
// 只负责 检测 + 创建 log（detected/confirmed）+ 飞书通知（可选），不推 MNS、不做下单
type PromoteDetectorService struct {
	strategyRepo    *repository.StrategyRepo
	authorVideoRepo *repository.AuthorVideoRepo
	videoStatRepo   *repository.VideoStatRepo
	promoteLogRepo  *repository.AutoPromoteLogRepo
	authorRepo      *repository.AuthorRepo
	sheetTabRepo    *repository.FeishuSheetTabRepo // 飞书 sheet tab（用于卡片链接）
	weixinClient    *weixin.Client
	notifier        *NotifierService // 飞书通知（复用现有 NotifierService）
	cfg             *config.Config
}

func NewPromoteDetectorService(
	strategyRepo *repository.StrategyRepo,
	authorVideoRepo *repository.AuthorVideoRepo,
	videoStatRepo *repository.VideoStatRepo,
	promoteLogRepo *repository.AutoPromoteLogRepo,
	authorRepo *repository.AuthorRepo,
	sheetTabRepo *repository.FeishuSheetTabRepo,
	weixinClient *weixin.Client,
	notifier *NotifierService,
	cfg *config.Config,
) *PromoteDetectorService {
	return &PromoteDetectorService{
		strategyRepo:    strategyRepo,
		authorVideoRepo: authorVideoRepo,
		videoStatRepo:   videoStatRepo,
		promoteLogRepo:  promoteLogRepo,
		authorRepo:      authorRepo,
		sheetTabRepo:    sheetTabRepo,
		weixinClient:    weixinClient,
		notifier:        notifier,
		cfg:             cfg,
	}
}

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
			log.Printf("service=promote-detector action=list_strategies error=%v", err)
			select {
			case <-ctx.Done():
				return s.summary("error", round, totalChecked, totalTriggered)
			case <-time.After(promotePollInterval):
				continue
			}
		}

		if len(strategies) == 0 {
			select {
			case <-ctx.Done():
				return s.summary("no_strategies", round, totalChecked, totalTriggered)
			case <-time.After(promotePollInterval):
				continue
			}
		}

		// 2. 并发处理各作者策略
		g, gCtx := errgroup.WithContext(ctx)
		sem := make(chan struct{}, maxConcurrency)

		for _, strategy := range strategies {
			strategy := strategy

			g.Go(func() error {
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-gCtx.Done():
					return gCtx.Err()
				}

				decisions, err := s.evaluateVideos(gCtx, strategy)
				if err != nil {
					log.Printf("service=promote-detector action=evaluate_videos author_id=%d error=%v", strategy.AuthorID, err)
					return nil
				}

				// 2c. 对每个命中的视频
				for _, d := range decisions {
					mu.Lock()
					totalChecked++
					mu.Unlock()

					// 并发去重：stat_id_current
					exists, _ := s.promoteLogRepo.ExistsByStatIDCurrent(d.StatIDCurrent)
					if exists {
						continue
					}

					// 视频级冷却
					cooldown := time.Duration(s.cfg.AutoPromoteVideoCooldownSec) * time.Second
					if cooldown <= 0 {
						cooldown = defaultVideoCooldown
					}
					recent, _ := s.promoteLogRepo.HasRecentPromote(d.AuthorVideoID, cooldown)
					if recent {
						continue
					}

					// 根据 notify_feishu 决定初始状态
					initialStatus := model.PromoteLogConfirmed // 默认自动确认
					if strategy.NotifyFeishu {
						initialStatus = model.PromoteLogDetected // 需要人工审核
					}

					// 创建 auto_promote_log
					promoteLog := s.buildPromoteLog(d, strategy, initialStatus)
					if initialStatus == model.PromoteLogConfirmed {
						now := time.Now()
						promoteLog.ConfirmedAt = &now // 自动确认记录时间
					}
					if err := s.promoteLogRepo.Create(promoteLog); err != nil {
						log.Printf("service=promote-detector action=create_log export_id=%s error=%v", d.ExportID, err)
						continue // 唯一索引冲突 = 已被另一次执行处理
					}

					// 推送飞书通知（始终推送，NotifyFeishu 控制卡片类型）
					if s.notifier != nil {
						// 查询飞书表格链接
						feishuSheetURL := s.buildFeishuSheetURL(d.AuthorID, d.ExportID)

						cardInfo := feishu.PromoteCardInfo{
							LogID:               promoteLog.ID,
							AuthorName:          d.AuthorName,
							Description:         d.Description,
							PromoteType:         d.PromoteType,
							HourlyPlay:          d.HourlyPlay,
							LikeRate:            d.LikeRate,
							ShareRate:           d.ShareRate,
							HourlyPlayThreshold: strategy.HourlyPlayThreshold,
							LikeRateThreshold:   strategy.LikeRateThreshold,
							ShareRateThreshold:  strategy.ShareRateThreshold,
							FeishuSheetURL:      feishuSheetURL,
							CurrentDate:         d.CurrentDate,
							CurrentPlay:         d.CurrentPlay,
							CurrentLike:         d.CurrentLike,
							CurrentShare:        d.CurrentShare,
							CurrentFollow:       d.CurrentFollow,
							CurrentComment:      d.CurrentComment,
							PrevDate:            d.PrevDate,
							PrevPlay:            d.PrevPlay,
							PrevLike:            d.PrevLike,
							PrevShare:           d.PrevShare,
							PrevFollow:          d.PrevFollow,
							PrevComment:         d.PrevComment,
						}
						if strategy.NotifyFeishu {
							// 需要审核 → 交互卡片（含确认/拒绝按钮）
							if err := s.notifier.SendPromoteCard(cardInfo); err != nil {
								log.Printf("service=promote-detector action=send_feishu author_video_id=%d error=%v", d.AuthorVideoID, err)
							}
						} else {
							// 自动确认 → 信息卡片（仅通知，无按钮）
							if err := s.notifier.SendPromoteInfoCard(cardInfo); err != nil {
								log.Printf("service=promote-detector action=send_info_card author_video_id=%d error=%v", d.AuthorVideoID, err)
							}
						}
					}

					mu.Lock()
					totalTriggered++
					mu.Unlock()
				}

				return nil
			})
		}

		g.Wait()

		select {
		case <-ctx.Done():
			return s.summary("done", round, totalChecked, totalTriggered)
		case <-time.After(promotePollInterval):
		}
	}
}

// evaluateVideos 评估作者所有视频是否满足投放条件
func (s *PromoteDetectorService) evaluateVideos(ctx context.Context, strategy model.AuthorPromoteStrategy) ([]PromoteDecision, error) {
	authorVideos, err := s.authorVideoRepo.ListByAuthorID(strategy.AuthorID)
	if err != nil {
		return nil, err
	}

	var decisions []PromoteDecision

	for _, av := range authorVideos {
		select {
		case <-ctx.Done():
			return decisions, ctx.Err()
		default:
		}

		// 倒序取水位线之后最新两条（无水位线则取全局最新两条）
		records, err := s.videoStatRepo.GetNextTwoByAuthorVideo(av.ID, av.Description, av.LastCheckedAt)
		if err != nil || len(records) == 0 {
			continue
		}

		prev := records[0]   // 较早
		latest := records[1] // 较新
		prevID := prev.ID
		var statIDPrev *int64 = &prevID

		// 更新水位线为较早记录的 collect_date
		if t, err := time.Parse("2006-01-02 15:04:05", prev.CollectDate); err == nil {
			_ = s.authorVideoRepo.TouchLastChecked(av.ID, t)
		} else {
			_ = s.authorVideoRepo.TouchLastChecked(av.ID, time.Now())
		}

		// 计算时间差（小时）
		var hours float64 = 1
		t0, err0 := time.Parse(time.DateTime, latest.CreatedAt.Format(time.DateTime))
		t1, err1 := time.Parse(time.DateTime, prev.CreatedAt.Format(time.DateTime))
		if err0 == nil && err1 == nil {
			hours = math.Max(t0.Sub(t1).Hours(), 0.01)
		}

		// 增量计算
		playDelta := latest.PlayCount - prev.PlayCount
		likeDelta := latest.LikeCount - prev.LikeCount
		shareDelta := latest.ShareCount - prev.ShareCount

		// 单小时播放量 = 播放增量 ÷ 时间差(小时)
		hourlyPlay := int(float64(playDelta) / hours)

		// 率指标 = 增量差 / 播放增量差（全部用增量）
		var likeRate, shareRate float64
		if playDelta > 0 {
			likeRate = float64(likeDelta) / float64(playDelta) * 100
			shareRate = float64(shareDelta) / float64(playDelta) * 100
		}

		log.Printf("service=promote-detector action=evaluate export_id=%s hourly_play=%d play_delta=%d hours=%.2f like_rate=%.3f%% share_rate=%.3f%% threshold_play=%d threshold_like=%.3f threshold_share=%.3f",
			av.ExportID, hourlyPlay, playDelta, hours, likeRate, shareRate,
			strategy.HourlyPlayThreshold, strategy.LikeRateThreshold, strategy.ShareRateThreshold)

		// 策略匹配
		promoteType := matchStrategy(hourlyPlay, likeRate, shareRate, strategy)

		if promoteType == "" {
			continue
		}

		log.Printf("service=promote-detector action=matched export_id=%s promote_type=%s author_id=%d", av.ExportID, promoteType, strategy.AuthorID)

		// raw_data
		videoRaw := []byte(av.RawData)
		var authorRaw []byte
		statRawData, _ := json.Marshal(records)

		authorName := ""
		author, _ := s.authorRepo.GetByID(strategy.AuthorID)
		if author != nil {
			authorRaw, _ = json.Marshal(author)
			authorName = author.Nickname
		}

		decisions = append(decisions, PromoteDecision{
			AuthorVideoID:  av.ID,
			AuthorID:       strategy.AuthorID,
			ExportID:       av.ExportID,
			AuthorName:     authorName,
			Description:    latest.Description,
			PromoteType:    promoteType,
			StatIDCurrent:  latest.ID,
			StatIDPrevious: statIDPrev,
			StatRawData:    statRawData,
			HourlyPlay:     hourlyPlay,
			LikeRate:       likeRate,
			ShareRate:      shareRate,
			CurrentDate:    latest.CollectDate,
			CurrentPlay:    latest.PlayCount,
			CurrentLike:    latest.LikeCount,
			CurrentShare:   latest.ShareCount,
			CurrentFollow:  latest.FollowCount,
			CurrentComment: latest.CommentCount,
			PrevDate:       prev.CollectDate,
			PrevPlay:       prev.PlayCount,
			PrevLike:       prev.LikeCount,
			PrevShare:      prev.ShareCount,
			PrevFollow:     prev.FollowCount,
			PrevComment:    prev.CommentCount,
			VideoRawData:   videoRaw,
			AuthorRawData:  authorRaw,
		})
	}

	return decisions, nil
}

// matchStrategy 策略匹配
func matchStrategy(hourlyPlay int, likeRate, shareRate float64, s model.AuthorPromoteStrategy) string {
	// 情况一优先：单小时播放 > 标准值×2 → 投点赞率(推荐)
	if hourlyPlay > s.HourlyPlayThreshold*2 {
		return "like_rate"
	}
	// 情况二：单小时播放 > 标准值，或（转发率 > 标准 且 点赞率 > 标准 且 播放量 > 标准×0.5）→ 投粉丝(关注)
	if hourlyPlay > s.HourlyPlayThreshold ||
		(likeRate > s.LikeRateThreshold && shareRate > s.ShareRateThreshold && hourlyPlay > s.HourlyPlayThreshold/2) {
		return "followers"
	}
	return ""
}

// buildPromoteLog 构建投放日志
func (s *PromoteDetectorService) buildPromoteLog(d PromoteDecision, strategy model.AuthorPromoteStrategy, status string) *model.AutoPromoteLog {
	promoteLog := &model.AutoPromoteLog{
		StrategyID:      strategy.ID,
		AuthorID:        strategy.AuthorID,
		AccountID:       strategy.AccountID,
		Platform:        strategy.Platform,
		AuthorVideoID:   d.AuthorVideoID,
		PromoteType:     d.PromoteType,
		StatIDCurrent:   d.StatIDCurrent,
		StatIDPrevious:  d.StatIDPrevious,
		StatRawData:     datatypes.JSON(d.StatRawData),
		HourlyPlayCount: &d.HourlyPlay,
		LikeRate:        &d.LikeRate,
		ShareRate:       &d.ShareRate,
		Status:          status,
		VideoRawData:    datatypes.JSON(d.VideoRawData),
		AuthorRawData:   datatypes.JSON(d.AuthorRawData),
	}
	return promoteLog
}

// summary 生成执行摘要
func (s *PromoteDetectorService) summary(reason string, round, checked, triggered int) string {
	msg, _ := json.Marshal(map[string]any{
		"reason":    reason,
		"rounds":    round,
		"checked":   checked,
		"triggered": triggered,
	})
	log.Printf("service=promote-detector action=summary %s", string(msg))
	return string(msg)
}

// buildFeishuSheetURL 构建飞书表格页链接（按视频发布日期的月份查询）
func (s *PromoteDetectorService) buildFeishuSheetURL(authorID int64, exportID string) string {
	if s.sheetTabRepo == nil || s.authorVideoRepo == nil {
		return ""
	}
	// 通过 author_video 获取视频发布时间，用发布月份查 sheet tab
	av, err := s.authorVideoRepo.GetByExportID(exportID)
	if err != nil || av == nil {
		return ""
	}
	month := time.Now().Format("2006-01") // fallback
	if ct := av.GetCreateTime(); ct != nil {
		month = ct.Format("2006-01")
	} else if len(av.PublishTime) >= 7 {
		month = av.PublishTime[:7]
	}
	tab, err := s.sheetTabRepo.GetByExportID(authorID, month, exportID)
	if err != nil || tab == nil {
		return ""
	}
	return fmt.Sprintf("https://feishu.cn/sheets/%s?sheet=%s", tab.SpreadsheetToken, tab.SheetID)
}
