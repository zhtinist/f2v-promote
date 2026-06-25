package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/pkg/feishu"
	"github.com/AiMarketool/f2v-promote/internal/repository"
)

const (
	autoStopExecTimeout          = 58 * time.Second
	autoStopPollInterval         = 15 * time.Second
	autoStopBatchSize            = 20
	defaultStopCoefficient       = 0.7  // 默认基线衰减系数（R_n/R ≤ 0.7）
	defaultStopDecayCoefficient  = 0.8  // 默认环比衰减系数（R_n/R_{n-1} ≤ 0.8）
	defaultFanCostThreshold      = 0.8  // 默认吸粉成本阈值：0.8 元/粉丝
	defaultStopProtectionMin     = 30   // 默认保护期：30 分钟
	weidouToYuan                 = 0.1  // 微信豆换算：1 微信豆 = 0.1 元
)

// AutoStopService 自动关停评估服务
type AutoStopService struct {
	promoteLogRepo  *repository.AutoPromoteLogRepo
	orderRepo       *repository.OrderRepo
	campaignRepo    *repository.CampaignRepo
	strategyRepo    *repository.StrategyRepo
	videoStatRepo   *repository.VideoStatRepo
	authorVideoRepo *repository.AuthorVideoRepo
	authorRepo      *repository.AuthorRepo
	sheetTabRepo    *repository.FeishuSheetTabRepo
	weixinClient    *weixin.Client
	notifier        *NotifierService
	cfg             *config.Config
}

func NewAutoStopService(
	promoteLogRepo *repository.AutoPromoteLogRepo,
	orderRepo *repository.OrderRepo,
	campaignRepo *repository.CampaignRepo,
	strategyRepo *repository.StrategyRepo,
	videoStatRepo *repository.VideoStatRepo,
	authorVideoRepo *repository.AuthorVideoRepo,
	authorRepo *repository.AuthorRepo,
	sheetTabRepo *repository.FeishuSheetTabRepo,
	weixinClient *weixin.Client,
	notifier *NotifierService,
	cfg *config.Config,
) *AutoStopService {
	return &AutoStopService{
		promoteLogRepo:  promoteLogRepo,
		orderRepo:       orderRepo,
		campaignRepo:    campaignRepo,
		strategyRepo:    strategyRepo,
		videoStatRepo:   videoStatRepo,
		authorVideoRepo: authorVideoRepo,
		authorRepo:      authorRepo,
		sheetTabRepo:    sheetTabRepo,
		weixinClient:    weixinClient,
		notifier:        notifier,
		cfg:             cfg,
	}
}

// Run 主入口（Cron 每分钟触发，58s 内循环）
func (s *AutoStopService) Run() string {
	ctx, cancel := context.WithTimeout(context.Background(), autoStopExecTimeout)
	defer cancel()

	totalChecked, totalStopped := 0, 0
	round := 0

	for {
		select {
		case <-ctx.Done():
			return s.summary("timeout", round, totalChecked, totalStopped)
		default:
		}

		round++

		logs, err := s.promoteLogRepo.ListForStopEvaluation(autoStopBatchSize)
		if err != nil {
			log.Printf("service=auto-stop action=list_evaluation error=%v", err)
			select {
			case <-ctx.Done():
				return s.summary("error", round, totalChecked, totalStopped)
			case <-time.After(autoStopPollInterval):
				continue
			}
		}

		if len(logs) == 0 {
			select {
			case <-ctx.Done():
				return s.summary("done", round, totalChecked, totalStopped)
			case <-time.After(autoStopPollInterval):
				continue
			}
		}

		log.Printf("service=auto-stop action=run round=%d log_count=%d", round, len(logs))

		for _, pl := range logs {
			select {
			case <-ctx.Done():
				return s.summary("timeout_mid", round, totalChecked, totalStopped)
			default:
			}

			totalChecked++
			if s.evaluate(ctx, pl) {
				totalStopped++
			}
		}

		select {
		case <-ctx.Done():
			return s.summary("done", round, totalChecked, totalStopped)
		case <-time.After(autoStopPollInterval):
		}
	}
}

// evaluate 评估单条 auto_promote_log，返回是否触发了关停
func (s *AutoStopService) evaluate(ctx context.Context, pl model.AutoPromoteLog) bool {
	if pl.OrderID == nil {
		return false
	}

	// 1. 查关联订单
	order, err := s.orderRepo.GetByID(*pl.OrderID)
	if err != nil || order == nil {
		return false
	}
	if order.Status != model.OrderStatusActive {
		return false
	}
	if order.PlatformOrderID == nil || *order.PlatformOrderID == "" {
		return false
	}

	accountID := strconv.FormatInt(order.AccountID, 10)
	promotionID := *order.PlatformOrderID

	// 2. 获取策略配置
	K := defaultStopCoefficient          // 基线衰减系数
	D := defaultStopDecayCoefficient     // 环比衰减系数
	fanThreshold := defaultFanCostThreshold
	protectionMin := defaultStopProtectionMin
	if strategy, err := s.strategyRepo.GetByID(pl.StrategyID); err == nil && strategy != nil {
		if strategy.StopCoefficient > 0 {
			K = strategy.StopCoefficient
		}
		if strategy.StopDecayCoefficient > 0 {
			D = strategy.StopDecayCoefficient
		}
		if strategy.FanCostThreshold > 0 {
			fanThreshold = strategy.FanCostThreshold
		}
		if strategy.StopProtectionMin > 0 {
			protectionMin = strategy.StopProtectionMin
		}
	}

	// 3. 保护期检查
	minAge := time.Duration(protectionMin) * time.Minute
	if time.Since(order.CreatedAt) < minAge {
		log.Printf("service=auto-stop action=skip_protection order=%d age=%s min=%dm",
			order.ID, time.Since(order.CreatedAt).Round(time.Second), protectionMin)
		return false
	}

	// 4. 规则一：指标衰减关停（转发率/点赞率/完播率）
	if reason := s.checkInteractionRate(pl, order.ID, K, D); reason != "" {
		return s.triggerStop(accountID, promotionID, order, pl, reason)
	}

	// 5. 规则二：吸粉成本关停（仅 followers 类型）
	if pl.PromoteType == "followers" {
		if reason := s.checkFanCost(accountID, promotionID, order.ID, fanThreshold); reason != "" {
			return s.triggerStop(accountID, promotionID, order, pl, reason)
		}
	}

	log.Printf("service=auto-stop action=evaluate order=%d promotion=%s result=pass", order.ID, promotionID)
	return false
}

// checkInteractionRate 规则一：指标衰减判断
// K = 基线衰减系数（R_n/R ≤ K，默认 0.7）
// D = 环比衰减系数（R_n/R_{n-1} ≤ D，默认 0.8）
// 需要 3 条数据：Sn(最新), Sn-1(中间), Sn-2(最旧) 用于环比
// 基线对 stat_raw_data [S0, S1] 用于基线衰减
func (s *AutoStopService) checkInteractionRate(pl model.AutoPromoteLog, orderID int64, K, D float64) string {
	// ── 基线对：解析 stat_raw_data [S0, S1] ──
	var baseline []model.VideoStat
	if err := json.Unmarshal(pl.StatRawData, &baseline); err != nil || len(baseline) < 2 {
		log.Printf("service=auto-stop action=check_interaction order=%d skip=parse_baseline err=%v count=%d",
			orderID, err, len(baseline))
		return ""
	}
	s0, s1 := baseline[0], baseline[1] // [older, newer]

	baselineDeltaPlay := s1.PlayCount - s0.PlayCount
	if baselineDeltaPlay <= 0 {
		log.Printf("service=auto-stop action=check_interaction order=%d skip=baseline_delta_play<=0", orderID)
		return ""
	}
	// 基线率
	baselineLikeRate := float64(s1.LikeCount-s0.LikeCount) / float64(baselineDeltaPlay)
	baselineShareRate := float64(s1.ShareCount-s0.ShareCount) / float64(baselineDeltaPlay)
	baselineCompletionRate := parseCompletionRate(s1.CompletionRate)

	// ── 当前：获取最新 3 条 video_stats ──
	if pl.AuthorVideoID == 0 {
		log.Printf("service=auto-stop action=check_interaction order=%d skip=no_author_video_id", orderID)
		return ""
	}
	avInfo, err := s.authorVideoRepo.GetVideoInfoMap([]int64{pl.AuthorVideoID})
	if err != nil || len(avInfo) == 0 {
		log.Printf("service=auto-stop action=check_interaction order=%d skip=author_video_not_found av_id=%d", orderID, pl.AuthorVideoID)
		return ""
	}
	exportID := avInfo[pl.AuthorVideoID].ExportID
	if exportID == "" {
		return ""
	}

	stats, err := s.videoStatRepo.GetLatestThreeByExportID(exportID)
	if err != nil || len(stats) < 3 {
		// 不足 3 条时降级为 2 条（仅基线衰减）
		if len(stats) >= 2 {
			return s.checkBaselineOnly(stats[0], stats[1], baselineLikeRate, baselineShareRate, baselineCompletionRate, orderID, K)
		}
		log.Printf("service=auto-stop action=check_interaction order=%d skip=insufficient_stats export_id=%s count=%d",
			orderID, exportID, len(stats))
		return ""
	}

	// stats: [newest, middle, oldest] — 按 created_at DESC
	sn, sn1, sn2 := stats[0], stats[1], stats[2]

	// 当前对 [Sn-1, Sn] 的增量率
	currentDeltaPlay := sn.PlayCount - sn1.PlayCount
	if currentDeltaPlay <= 0 {
		log.Printf("service=auto-stop action=check_interaction order=%d skip=current_delta_play<=0", orderID)
		return ""
	}
	currentLikeRate := float64(sn.LikeCount-sn1.LikeCount) / float64(currentDeltaPlay)
	currentShareRate := float64(sn.ShareCount-sn1.ShareCount) / float64(currentDeltaPlay)
	currentCompletionRate := parseCompletionRate(sn.CompletionRate)

	// 前一对 [Sn-2, Sn-1] 的增量率
	prevDeltaPlay := sn1.PlayCount - sn2.PlayCount
	var prevLikeRate, prevShareRate float64
	if prevDeltaPlay > 0 {
		prevLikeRate = float64(sn1.LikeCount-sn2.LikeCount) / float64(prevDeltaPlay)
		prevShareRate = float64(sn1.ShareCount-sn2.ShareCount) / float64(prevDeltaPlay)
	}
	prevCompletionRate := parseCompletionRate(sn1.CompletionRate)

	log.Printf("service=auto-stop action=check_interaction order=%d K=%.2f D=%.2f "+
		"baseline[like=%.4f share=%.4f comp=%.4f] current[like=%.4f share=%.4f comp=%.4f] prev[like=%.4f share=%.4f comp=%.4f]",
		orderID, K, D,
		baselineLikeRate, baselineShareRate, baselineCompletionRate,
		currentLikeRate, currentShareRate, currentCompletionRate,
		prevLikeRate, prevShareRate, prevCompletionRate)

	// ── 环比衰减判断（R_n/R_{n-1} ≤ D）──
	if prevLikeRate > 0 && currentLikeRate/prevLikeRate <= D {
		return fmt.Sprintf("环比衰减关停: 点赞率 %.4f→%.4f (比值%.2f≤%.2f)",
			prevLikeRate, currentLikeRate, currentLikeRate/prevLikeRate, D)
	}
	if prevShareRate > 0 && currentShareRate/prevShareRate <= D {
		return fmt.Sprintf("环比衰减关停: 转发率 %.4f→%.4f (比值%.2f≤%.2f)",
			prevShareRate, currentShareRate, currentShareRate/prevShareRate, D)
	}
	if prevCompletionRate > 0 && currentCompletionRate/prevCompletionRate <= D {
		return fmt.Sprintf("环比衰减关停: 完播率 %.4f→%.4f (比值%.2f≤%.2f)",
			prevCompletionRate, currentCompletionRate, currentCompletionRate/prevCompletionRate, D)
	}

	// ── 基线衰减判断（R_n/R ≤ K）──
	if baselineLikeRate > 0 && currentLikeRate/baselineLikeRate <= K {
		return fmt.Sprintf("基线衰减关停: 点赞率 %.4f→%.4f (比值%.2f≤%.2f)",
			baselineLikeRate, currentLikeRate, currentLikeRate/baselineLikeRate, K)
	}
	if baselineShareRate > 0 && currentShareRate/baselineShareRate <= K {
		return fmt.Sprintf("基线衰减关停: 转发率 %.4f→%.4f (比值%.2f≤%.2f)",
			baselineShareRate, currentShareRate, currentShareRate/baselineShareRate, K)
	}
	if baselineCompletionRate > 0 && currentCompletionRate/baselineCompletionRate <= K {
		return fmt.Sprintf("基线衰减关停: 完播率 %.4f→%.4f (比值%.2f≤%.2f)",
			baselineCompletionRate, currentCompletionRate, currentCompletionRate/baselineCompletionRate, K)
	}

	return ""
}

// checkBaselineOnly 仅 2 条数据时只做基线衰减
func (s *AutoStopService) checkBaselineOnly(sn, sn1 model.VideoStat, baselineLikeRate, baselineShareRate, baselineCompletionRate float64, orderID int64, K float64) string {
	currentDeltaPlay := sn.PlayCount - sn1.PlayCount
	if currentDeltaPlay <= 0 {
		return ""
	}
	currentLikeRate := float64(sn.LikeCount-sn1.LikeCount) / float64(currentDeltaPlay)
	currentShareRate := float64(sn.ShareCount-sn1.ShareCount) / float64(currentDeltaPlay)
	currentCompletionRate := parseCompletionRate(sn.CompletionRate)

	if baselineLikeRate > 0 && currentLikeRate/baselineLikeRate <= K {
		return fmt.Sprintf("基线衰减关停: 点赞率 %.4f→%.4f (比值%.2f≤%.2f)",
			baselineLikeRate, currentLikeRate, currentLikeRate/baselineLikeRate, K)
	}
	if baselineShareRate > 0 && currentShareRate/baselineShareRate <= K {
		return fmt.Sprintf("基线衰减关停: 转发率 %.4f→%.4f (比值%.2f≤%.2f)",
			baselineShareRate, currentShareRate, currentShareRate/baselineShareRate, K)
	}
	if baselineCompletionRate > 0 && currentCompletionRate/baselineCompletionRate <= K {
		return fmt.Sprintf("基线衰减关停: 完播率 %.4f→%.4f (比值%.2f≤%.2f)",
			baselineCompletionRate, currentCompletionRate, currentCompletionRate/baselineCompletionRate, K)
	}
	return ""
}

// checkFanCost 规则二：单粉成本超过策略阈值（数据源 = GetPlanDetail）
func (s *AutoStopService) checkFanCost(accountID, promotionID string, orderID int64, threshold float64) string {
	detail, err := s.weixinClient.GetPlanDetail(accountID, promotionID)
	if err != nil {
		log.Printf("service=auto-stop action=check_fan_cost order=%d error=%v", orderID, err)
		return ""
	}

	if detail.FocusNum <= 0 {
		return ""
	}

	costWeidou, _ := strconv.ParseFloat(detail.Cost, 64)
	costYuan := costWeidou * weidouToYuan
	costPerFan := costYuan / float64(detail.FocusNum)

	log.Printf("service=auto-stop action=check_fan_cost order=%d cost_weidou=%.2f cost_yuan=%.2f focus=%d cost_per_fan=%.2f threshold=%.2f",
		orderID, costWeidou, costYuan, detail.FocusNum, costPerFan, threshold)

	if costPerFan > threshold {
		return fmt.Sprintf("吸粉成本关停: %.2f元/%d粉=%.2f元/粉 (阈值%.1f元/粉)",
			costYuan, detail.FocusNum, costPerFan, threshold)
	}
	return ""
}

// triggerStop 执行关停 + 飞书通知
func (s *AutoStopService) triggerStop(accountID, promotionID string, order *model.Order, pl model.AutoPromoteLog, reason string) bool {
	log.Printf("service=auto-stop action=trigger_stop order=%d promotion=%s reason=%s", order.ID, promotionID, reason)

	if err := s.weixinClient.ClosePlan(accountID, promotionID); err != nil {
		log.Printf("service=auto-stop action=close_plan order=%d promotion=%s error=%v", order.ID, promotionID, err)
		return false // 关停失败，下次轮询重试
	}

	_ = s.orderRepo.UpdateStatus(order.ID, model.OrderStatusClosed, &reason)
	_ = s.promoteLogRepo.UpdateStatusByOrderID(order.ID, model.OrderStatusClosed, &reason)
	if s.campaignRepo != nil && order.CampaignID > 0 {
		_ = s.campaignRepo.UpdateStatus(order.CampaignID, model.OrderStatusClosed, &reason)
	}
	log.Printf("service=auto-stop action=closed order=%d promotion=%s reason=%s", order.ID, promotionID, reason)

	// 飞书关停通知
	if s.notifier != nil {
		description := ""
		exportID := ""
		var authorID int64
		if avInfo, err := s.authorVideoRepo.GetVideoInfoMap([]int64{pl.AuthorVideoID}); err == nil {
			if info, ok := avInfo[pl.AuthorVideoID]; ok {
				description = info.Description
				exportID = info.ExportID
			}
		}

		// 获取作者名称
		authorName := ""
		authorID = pl.AuthorID
		if s.authorRepo != nil && authorID > 0 {
			if author, err := s.authorRepo.GetByID(authorID); err == nil && author != nil {
				authorName = author.Nickname
			}
		}

		// 构建飞书表格链接
		feishuSheetURL := ""
		if s.sheetTabRepo != nil && authorID > 0 && exportID != "" {
			// 通过 author_video 获取视频发布时间，用发布月份查 sheet tab
			if av, err := s.authorVideoRepo.GetByExportID(exportID); err == nil && av != nil {
				month := time.Now().Format("2006-01") // fallback
				if ct := av.GetCreateTime(); ct != nil {
					month = ct.Format("2006-01")
				} else if len(av.PublishTime) >= 7 {
					month = av.PublishTime[:7]
				}
				if tab, err := s.sheetTabRepo.GetByExportID(authorID, month, exportID); err == nil && tab != nil {
					feishuSheetURL = fmt.Sprintf("https://feishu.cn/sheets/%s?sheet=%s", tab.SpreadsheetToken, tab.SheetID)
				}
			}
		}

		if err := s.notifier.SendStopCard(feishu.StopCardInfo{
			AuthorName:     authorName,
			Description:    description,
			PromoteType:    pl.PromoteType,
			OrderID:        order.ID,
			PromotionID:    promotionID,
			Reason:         reason,
			FeishuSheetURL: feishuSheetURL,
		}); err != nil {
			log.Printf("service=auto-stop action=send_stop_card order=%d error=%v", order.ID, err)
		}
	}

	return true
}

func (s *AutoStopService) summary(reason string, round, checked, stopped int) string {
	msg, _ := json.Marshal(map[string]any{
		"reason":  reason,
		"rounds":  round,
		"checked": checked,
		"stopped": stopped,
	})
	log.Printf("service=auto-stop action=summary %s", string(msg))
	return string(msg)
}

// parseCompletionRate 解析完播率字符串 "17.45%" → 0.1745
func parseCompletionRate(s string) float64 {
	s = strings.TrimSpace(s)
	s = strings.TrimSuffix(s, "%")
	if s == "" {
		return 0
	}
	v, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return v / 100
}
