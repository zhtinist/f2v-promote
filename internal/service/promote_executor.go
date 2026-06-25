package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/pkg/mns"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"gorm.io/datatypes"
)

// PromoteExecutorService 执行服务（MNS Consumer 使用）
type PromoteExecutorService struct {
	promoteLogRepo *repository.AutoPromoteLogRepo
	campaignRepo   *repository.CampaignRepo
	orderRepo      *repository.OrderRepo
	authorRepo     *repository.AuthorRepo
	weixinClient   *weixin.Client
	openAI         *OpenAIService
	cfg            *config.Config
}

func NewPromoteExecutorService(
	promoteLogRepo *repository.AutoPromoteLogRepo,
	campaignRepo *repository.CampaignRepo,
	orderRepo *repository.OrderRepo,
	authorRepo *repository.AuthorRepo,
	weixinClient *weixin.Client,
	openAI *OpenAIService,
	cfg *config.Config,
) *PromoteExecutorService {
	return &PromoteExecutorService{
		promoteLogRepo: promoteLogRepo,
		campaignRepo:   campaignRepo,
		orderRepo:      orderRepo,
		authorRepo:     authorRepo,
		weixinClient:   weixinClient,
		openAI:         openAI,
		cfg:            cfg,
	}
}

// Execute 消费单条 MNS 消息（幂等防护）
func (s *PromoteExecutorService) Execute(ctx context.Context, msg mns.PromoteMessage) error {
	// 1. CAS 原子更新 queued → running（幂等防护第二层）
	affected, err := s.promoteLogRepo.MarkAsRunning(msg.PromoteLogID)
	if err != nil {
		return fmt.Errorf("cas to running: %w", err)
	}
	if affected == 0 {
		// 已被其他 Consumer 实例处理，跳过
		log.Printf("service=promote-executor action=skip_duplicate log_id=%d", msg.PromoteLogID)
		return nil
	}

	// 2. 查 auto_promote_log → 校验 queued_at 一致性
	promoteLog, err := s.promoteLogRepo.GetByID(msg.PromoteLogID)
	if err != nil || promoteLog == nil {
		return fmt.Errorf("invalid promote log: %d", msg.PromoteLogID)
	}
	if promoteLog.QueuedAt != nil && !promoteLog.QueuedAt.Equal(msg.QueuedAt) {
		// 消息版本不一致，可能是过期的 MNS 重复消息
		log.Printf("service=promote-executor action=skip_stale log_id=%d msg_queued_at=%v db_queued_at=%v",
			msg.PromoteLogID, msg.QueuedAt, promoteLog.QueuedAt)
		return nil
	}

	// 3. defer: 任何 error 都标记为 failed
	var execErr error
	defer func() {
		if execErr != nil {
			errMsg := execErr.Error()
			_ = s.promoteLogRepo.UpdateStatus(promoteLog.ID, model.PromoteLogFailed, map[string]any{
				"error_msg": errMsg,
			})
		}
	}()

	// 4. 从 log 获取视频描述作为 script
	script := extractDescription(promoteLog.VideoRawData)
	if script == "" {
		script = "视频推广"
	}

	// 5. 调 OpenAI 生成 tag_groups
	accountIDStr := strconv.FormatInt(promoteLog.AccountID, 10)
	flatTags, execErr := s.weixinClient.GetFlatTags(accountIDStr)
	if execErr != nil {
		return execErr
	}

	tagGroups, execErr := s.openAI.GenerateZhugeTags(script, flatTags, s.cfg.TagGroupCount)
	if execErr != nil {
		return execErr
	}

	tagGroupsJSON, _ := json.Marshal(tagGroups)
	log.Printf("service=promote-executor action=generate_tags log_id=%d tag_count=%d", msg.PromoteLogID, len(tagGroups))

	// 6. 获取账号配置（tsUserID, groupID）
	_, wCfg, _, execErr := s.weixinClient.GetTokenForAccount(accountIDStr)
	if execErr != nil {
		return fmt.Errorf("promote-executor: get account config: %w", execErr)
	}

	author, execErr := s.authorRepo.GetByID(promoteLog.AuthorID)
	if execErr != nil {
		return fmt.Errorf("promote-executor: get author: %w", execErr)
	}

	// 7. 创建 Campaign（userID=0 表示系统自动）
	campaign, execErr := s.campaignRepo.Create(author.ID, promoteLog.AccountID, script, datatypes.JSON(tagGroupsJSON))
	if execErr != nil {
		return execErr
	}

	// 8. 遍历 tagGroups，逐个创建 order + 调诸葛 CreatePlan
	var lastOrderID int64
	for i, tg := range tagGroups {
		tgJSON, _ := json.Marshal(tg)

		// 8a. 创建 Order（每个 tagGroup 一条）
		order, err := s.orderRepo.CreateZhuge(campaign.ID, author.ID, promoteLog.AccountID, datatypes.JSON(tgJSON), nil)
		if err != nil {
			log.Printf("service=promote-executor action=create_order log_id=%d group=%d error=%v", msg.PromoteLogID, i, err)
			continue
		}
		lastOrderID = order.ID

		// 8b. 构建 plan_data（复用 weixin.BuildPlanData）
		planData := weixin.BuildPlanData(tg,
			json.RawMessage(promoteLog.VideoRawData),
			json.RawMessage(promoteLog.AuthorRawData),
			s.cfg, wCfg.TsUserID, wCfg.GroupID,
			promoteLog.PromoteType,
		)

		// 8b+. 保存请求参数到 order
		if planJSON, err := json.Marshal(planData); err == nil {
			_ = s.orderRepo.UpdateCreateRequest(order.ID, planJSON)
		}

		// 8c. 提交到诸葛
		batchID, _, err := s.weixinClient.CreatePlan(accountIDStr, planData)
		if err != nil {
			log.Printf("service=promote-executor action=create_plan log_id=%d group=%d error=%v", msg.PromoteLogID, i, err)
			reason := fmt.Sprintf("CreatePlan failed: %v", err)
			_ = s.orderRepo.UpdateStatus(order.ID, model.OrderStatusFailed, &reason)
			continue
		}

		if batchID != "" {
			_ = s.orderRepo.UpdateZhugeSubmitted(order.ID, batchID, "")
		}
		log.Printf("service=promote-executor action=plan_created log_id=%d group=%d/%d order_id=%d batch_id=%s",
			msg.PromoteLogID, i+1, len(tagGroups), order.ID, batchID)
	}

	// 9. 全部提交完成 → completed
	_ = s.promoteLogRepo.UpdateStatus(promoteLog.ID, model.PromoteLogCompleted, map[string]any{
		"campaign_id": campaign.ID,
		"order_id":    lastOrderID,
		"tag_groups":  datatypes.JSON(tagGroupsJSON),
	})

	log.Printf("service=promote-executor action=completed log_id=%d campaign_id=%d orders=%d", msg.PromoteLogID, campaign.ID, len(tagGroups))
	return nil
}

// extractDescription 从 video raw_data JSON 提取 description
func extractDescription(rawData datatypes.JSON) string {
	if len(rawData) == 0 {
		return ""
	}
	var data map[string]any
	if err := json.Unmarshal(rawData, &data); err != nil {
		return ""
	}
	if desc, ok := data["description"].(string); ok {
		return desc
	}
	return ""
}
