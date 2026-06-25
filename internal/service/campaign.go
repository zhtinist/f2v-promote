package service

import (
	"encoding/json"
	"fmt"
	"log"
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"github.com/AiMarketool/f2v-promote/internal/types"
	"gorm.io/datatypes"
)

// CampaignService orchestrates campaign creation, order submission, and payment tracking.
type CampaignService struct {
	campaignRepo   *repository.CampaignRepo
	orderRepo      *repository.OrderRepo
	auditRepo      *repository.AuditRepo
	promoteLogRepo *repository.AutoPromoteLogRepo
	weixinClient   *weixin.Client
	openAI         *OpenAIService
	notifier       *NotifierService
	cfg            *config.Config
}

// NewCampaignService creates a new CampaignService.
func NewCampaignService(
	campaignRepo *repository.CampaignRepo,
	orderRepo *repository.OrderRepo,
	auditRepo *repository.AuditRepo,
	promoteLogRepo *repository.AutoPromoteLogRepo,
	weixinClient *weixin.Client,
	openAI *OpenAIService,
	notifier *NotifierService,
	cfg *config.Config,
) *CampaignService {
	return &CampaignService{
		campaignRepo:   campaignRepo,
		orderRepo:      orderRepo,
		auditRepo:      auditRepo,
		promoteLogRepo: promoteLogRepo,
		weixinClient:   weixinClient,
		openAI:         openAI,
		notifier:       notifier,
		cfg:            cfg,
	}
}

func (s *CampaignService) auditLog(userID *int64, action string, detail any) {
	b, err := json.Marshal(detail)
	if err != nil {
		log.Printf("service=campaign action=audit_log error=%v", err)
		return
	}
	if err := s.auditRepo.Add(userID, action, datatypes.JSON(b)); err != nil {
		log.Printf("service=campaign action=audit_log error=%v", err)
	}
}

// CreateWithAITags generates AI tag groups, creates a campaign, and returns the campaign + tags.
func (s *CampaignService) CreateWithAITags(userID, accountID int64, accountIDStr, script string, costThreshold float64, tagGroupCount int) (*model.Campaign, []weixin.TagGroup, map[string]model.TagMeta, error) {
	flatTags, err := s.weixinClient.GetFlatTags(accountIDStr)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("campaign: get flat tags: %w", err)
	}

	tagGroups, err := s.openAI.GenerateZhugeTags(script, flatTags, tagGroupCount)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("campaign: generate tags: %w", err)
	}

	tagGroupsJSON, err := json.Marshal(tagGroups)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("campaign: marshal tag groups: %w", err)
	}

	campaign, err := s.campaignRepo.Create(userID, accountID, script, datatypes.JSON(tagGroupsJSON))
	if err != nil {
		return nil, nil, nil, fmt.Errorf("campaign: create: %w", err)
	}

	s.auditLog(&userID, "campaign_created", map[string]any{
		"campaign_id":     campaign.ID,
		"tag_group_count": len(tagGroups),
	})

	return campaign, tagGroups, flatTags, nil
}

// Confirm creates DB orders for each tag group in the campaign.
func (s *CampaignService) Confirm(userID, campaignID int64) ([]types.ConfirmOrderItem, error) {
	campaign, err := s.campaignRepo.GetByID(campaignID)
	if err != nil {
		return nil, fmt.Errorf("campaign: get by id: %w", err)
	}
	if campaign == nil {
		return nil, fmt.Errorf("campaign: not found: %d", campaignID)
	}
	if campaign.UserID != userID {
		return nil, fmt.Errorf("campaign: unauthorized")
	}

	var rawGroups []json.RawMessage
	if err := json.Unmarshal(campaign.TagGroups, &rawGroups); err != nil {
		return nil, fmt.Errorf("campaign: unmarshal tag groups: %w", err)
	}

	orders := make([]types.ConfirmOrderItem, 0, len(rawGroups))
	for _, tgRaw := range rawGroups {
		order, err := s.orderRepo.CreateZhuge(campaignID, userID, campaign.AccountID, datatypes.JSON(tgRaw), nil)
		if err != nil {
			return nil, fmt.Errorf("campaign: create order: %w", err)
		}

		orders = append(orders, types.ConfirmOrderItem{
			ID:       order.ID,
			TagGroup: tgRaw,
			Status:   order.Status,
		})
	}

	if err := s.campaignRepo.UpdateStatus(campaignID, "running", nil); err != nil {
		log.Printf("service=campaign action=update_status campaign_id=%d error=%v", campaignID, err)
	}

	s.auditLog(&userID, "campaign_confirmed", map[string]any{
		"campaign_id": campaignID,
		"order_count": len(orders),
	})

	return orders, nil
}

// SubmitOrder submits a single order to the platform.
func (s *CampaignService) SubmitOrder(userID, orderID int64, video, author json.RawMessage) (*types.SubmitOrderResult, error) {
	order, err := s.orderRepo.GetByID(orderID)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}
	if order == nil {
		return nil, fmt.Errorf("order not found: %d", orderID)
	}
	if order.UserID != userID {
		return nil, fmt.Errorf("unauthorized")
	}
	if order.Status != model.OrderStatusInit {
		return nil, fmt.Errorf("订单状态为 %s，无法提交", order.Status)
	}

	var tagGroup weixin.TagGroup
	if err := json.Unmarshal(order.TagGroup, &tagGroup); err != nil {
		return nil, fmt.Errorf("unmarshal tag group: %w", err)
	}

	accountIDStr := strconv.FormatInt(order.AccountID, 10)
	_, wCfg, _, err := s.weixinClient.GetTokenForAccount(accountIDStr)
	if err != nil {
		return nil, fmt.Errorf("获取账号信息失败: %w", err)
	}

	planData := weixin.BuildPlanData(tagGroup, video, author, s.cfg, wCfg.TsUserID, wCfg.GroupID, "followers")

	// 保存请求参数到 order
	if planJSON, err := json.Marshal(planData); err == nil {
		_ = s.orderRepo.UpdateCreateRequest(orderID, planJSON)
	}

	log.Printf("service=campaign action=submit_order order=%d account=%s ts_user_id=%s group_id=%s", orderID, wCfg.Account, wCfg.TsUserID, wCfg.GroupID)
	batchID, createResp, err := s.weixinClient.CreatePlan(accountIDStr, planData)
	if err != nil {
		errMsg := fmt.Sprintf("创建计划失败: %v", err)
		reason := errMsg
		_ = s.orderRepo.UpdateStatus(orderID, model.OrderStatusFailed, &reason)
		_ = s.orderRepo.AppendQueryResponse(orderID, map[string]any{"error": errMsg})
		return &types.SubmitOrderResult{
			OrderID: orderID,
			Status:  model.OrderStatusFailed,
			Error:   errMsg,
		}, nil
	}

	_ = s.orderRepo.UpdateZhugeSubmitted(orderID, batchID, "")
	_ = s.orderRepo.AppendQueryResponse(orderID, map[string]any{"create": createResp})

	return &types.SubmitOrderResult{
		OrderID: orderID,
		Status:  model.OrderStatusPending,
		BatchID: batchID,
	}, nil
}

// CheckPayment returns the current payment/order status from DB.
func (s *CampaignService) CheckPayment(userID, orderID int64) (*types.CheckPaymentResult, error) {
	order, err := s.orderRepo.GetByID(orderID)
	if err != nil || order == nil {
		return nil, fmt.Errorf("order not found")
	}
	if order.UserID != userID {
		return nil, fmt.Errorf("forbidden")
	}

	paid := order.Status == model.OrderStatusActive || order.Status == model.OrderStatusReview || order.Status == model.OrderStatusClosed

	return &types.CheckPaymentResult{
		OrderID:         orderID,
		Status:          order.Status,
		Paid:            paid,
		PlatformOrderID: order.PlatformOrderID,
		PayURL:          order.PayURL,
		CloseReason:     order.CloseReason,
	}, nil
}

// GetDetail returns cached plan detail from DB.
func (s *CampaignService) GetDetail(userID, orderID int64) (*types.OrderDetailResult, error) {
	order, err := s.orderRepo.GetByID(orderID)
	if err != nil || order == nil {
		return nil, fmt.Errorf("order not found")
	}
	if order.UserID != userID {
		return nil, fmt.Errorf("forbidden")
	}

	if len(order.LatestDetail) == 0 {
		return &types.OrderDetailResult{
			OrderID: orderID,
			Msg:     "数据同步中，请稍后刷新",
		}, nil
	}

	var detail weixin.PlanDetail
	if err := json.Unmarshal(order.LatestDetail, &detail); err != nil {
		log.Printf("service=campaign action=get_detail order=%d error=%v", orderID, err)
		return &types.OrderDetailResult{
			OrderID: orderID,
			Msg:     "数据解析失败",
		}, nil
	}

	return &types.OrderDetailResult{
		OrderID: orderID,
		Detail:  &detail,
		QueryAt: order.QueryAt,
	}, nil
}

// GetRecord returns cached plan record from DB.
func (s *CampaignService) GetRecord(userID, orderID int64) (*types.OrderRecordResult, error) {
	order, err := s.orderRepo.GetByID(orderID)
	if err != nil || order == nil {
		return nil, fmt.Errorf("order not found")
	}
	if order.UserID != userID {
		return nil, fmt.Errorf("forbidden")
	}

	if len(order.LatestRecord) == 0 {
		return &types.OrderRecordResult{
			OrderID: orderID,
			Msg:     "数据同步中，请稍后刷新",
		}, nil
	}

	var record []weixin.PlanRecord
	if err := json.Unmarshal(order.LatestRecord, &record); err != nil {
		log.Printf("service=campaign action=get_record order=%d error=%v", orderID, err)
		return &types.OrderRecordResult{
			OrderID: orderID,
			Msg:     "数据解析失败",
		}, nil
	}

	return &types.OrderRecordResult{
		OrderID: orderID,
		Record:  record,
		QueryAt: order.QueryAt,
	}, nil
}

// StopOrder 手动停止投放：调用诸葛 ClosePlan 并更新 DB 状态
func (s *CampaignService) StopOrder(userID, orderID int64) error {
	order, err := s.orderRepo.GetByID(orderID)
	if err != nil || order == nil {
		return fmt.Errorf("order not found")
	}
	if order.UserID != userID {
		return fmt.Errorf("forbidden")
	}
	if order.Status != model.OrderStatusActive {
		return fmt.Errorf("订单状态为 %s，仅加热中的订单可以停止", order.Status)
	}
	if order.PlatformOrderID == nil || *order.PlatformOrderID == "" {
		return fmt.Errorf("订单缺少平台订单号，无法停止")
	}

	accountIDStr := strconv.FormatInt(order.AccountID, 10)
	promotionID := *order.PlatformOrderID

	if err := s.weixinClient.ClosePlan(accountIDStr, promotionID); err != nil {
		return fmt.Errorf("停止投放失败: %w", err)
	}

	reason := "用户手动停止投放"
	if err := s.orderRepo.UpdateStatus(orderID, model.OrderStatusClosed, &reason); err != nil {
		log.Printf("service=campaign action=stop_order order=%d error=%v", orderID, err)
		return fmt.Errorf("更新订单状态失败: %w", err)
	}

	s.auditLog(&userID, "order_stopped", map[string]any{
		"order_id":     orderID,
		"promotion_id": promotionID,
	})

	// 同步关联投放日志状态
	if s.promoteLogRepo != nil {
		stopMsg := "用户手动停止投放"
		if err := s.promoteLogRepo.UpdateStatusByOrderID(orderID, model.OrderStatusClosed, &stopMsg); err != nil {
			log.Printf("service=campaign action=sync_promote_log order=%d error=%v", orderID, err)
		}
	}

	// 同步活动状态
	if order.CampaignID > 0 {
		_ = s.campaignRepo.UpdateStatus(order.CampaignID, model.OrderStatusClosed, &reason)
	}

	log.Printf("service=campaign action=stop_order order=%d promotion=%s result=success", orderID, promotionID)
	return nil
}
