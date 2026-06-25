package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
	"github.com/AiMarketool/f2v-promote/internal/config"
	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/internal/repository"
	"gorm.io/datatypes"
)

const (
	pollInterval = 15 * time.Second
	execTimeout  = 58 * time.Second
	batchSize    = 20
	minQueryGap  = 15 * time.Second
)

type CheckOrderService struct {
	orderRepo      *repository.OrderRepo
	campaignRepo   *repository.CampaignRepo
	promoteLogRepo *repository.AutoPromoteLogRepo
	weixinClient   *weixin.Client
	cfg            *config.Config
}

func NewCheckOrderService(
	orderRepo *repository.OrderRepo,
	campaignRepo *repository.CampaignRepo,
	promoteLogRepo *repository.AutoPromoteLogRepo,
	weixinClient *weixin.Client,
	cfg *config.Config,
) *CheckOrderService {
	return &CheckOrderService{
		orderRepo:      orderRepo,
		campaignRepo:   campaignRepo,
		promoteLogRepo: promoteLogRepo,
		weixinClient:   weixinClient,
		cfg:            cfg,
	}
}

// updateOrderStatus 更新订单状态并同步投放日志、活动状态（保持一致）
func (c *CheckOrderService) updateOrderStatus(orderID, campaignID int64, status string, reason *string) {
	_ = c.orderRepo.UpdateStatus(orderID, status, reason)

	if c.promoteLogRepo != nil {
		_ = c.promoteLogRepo.UpdateStatusByOrderID(orderID, status, reason)
	}
	if c.campaignRepo != nil && campaignID > 0 {
		_ = c.campaignRepo.UpdateStatus(campaignID, status, reason)
	}
}

func (c *CheckOrderService) Run() string {
	ctx, cancel := context.WithTimeout(context.Background(), execTimeout)
	defer cancel()

	totalChecked, totalUpdated := 0, 0
	round := 0

	for {
		select {
		case <-ctx.Done():
			return c.summary("timeout", round, totalChecked, totalUpdated)
		default:
		}

		round++
		orders, err := c.orderRepo.GetNeedPolling(batchSize, minQueryGap)
		if err != nil {
			log.Printf("service=cron action=run round=%d error=%v", round, err)
			return c.summary("db_error", round, totalChecked, totalUpdated)
		}

		if len(orders) == 0 {
			select {
			case <-ctx.Done():
				return c.summary("done", round, totalChecked, totalUpdated)
			case <-time.After(pollInterval):
				continue
			}
		}

		log.Printf("service=cron action=run round=%d order_count=%d", round, len(orders))

		for _, order := range orders {
			select {
			case <-ctx.Done():
				return c.summary("timeout_mid", round, totalChecked, totalUpdated)
			default:
			}

			updated := c.pollOrder(order)
			totalChecked++
			if updated {
				totalUpdated++
			}
			_ = c.orderRepo.TouchQueryAt(order.ID)
		}

		select {
		case <-ctx.Done():
			return c.summary("done", round, totalChecked, totalUpdated)
		case <-time.After(pollInterval):
		}
	}
}

func (c *CheckOrderService) summary(reason string, round, checked, updated int) string {
	result := fmt.Sprintf("%s: %d rounds, checked %d, updated %d", reason, round, checked, updated)
	log.Printf("service=cron action=summary reason=%s round=%d checked=%d updated=%d", reason, round, checked, updated)
	return result
}

func (c *CheckOrderService) pollOrder(order model.Order) bool {
	sid := strconv.FormatInt(order.ID, 10)
	accountIDStr := strconv.FormatInt(order.AccountID, 10)

	// 超时保护：pending/init 状态超过 5 分钟未支付，直接标记失败
	// if (order.Status == model.OrderStatusPending || order.Status == model.OrderStatusInit) &&
	// 	time.Since(order.CreatedAt) > 5*time.Minute {
	// 	reason := fmt.Sprintf("支付超时: 创建超过5分钟未完成支付 (status=%s)", order.Status)
	// 	_ = c.orderRepo.UpdateStatus(order.ID, model.OrderStatusFailed, &reason)
	// 	log.Printf("service=cron action=timeout_close order=%s status=%s age=%s", sid, order.Status, time.Since(order.CreatedAt).Round(time.Second))
	// 	return true
	// }

	promotionID := ""
	if order.PlatformOrderID != nil {
		promotionID = *order.PlatformOrderID
	}

	if promotionID == "" && order.BatchID != nil && *order.BatchID != "" {
		promotionID = c.tryFetchPlatformOrder(order)
	}

	if promotionID == "" {
		log.Printf("service=cron action=poll_order order=%s status=%s result=skip reason=no_promotion_id", sid, order.Status)
		return false
	}

	log.Printf("service=cron action=poll_order order=%s promotion=%s status=%s", sid, promotionID, order.Status)

	detail, err := c.weixinClient.GetPlanDetail(accountIDStr, promotionID)
	if err != nil {
		log.Printf("service=cron action=get_plan_detail order=%s promotion=%s error=%v", sid, promotionID, err)
		return false
	}

	log.Printf("service=cron action=get_plan_detail order=%s promotion=%s response={status=%d auto_close=%d label=%s cost=%s roi=%s view=%d click=%d focus=%d order_num=%d}",
		sid, promotionID, detail.Status, detail.AutoCloseExecStatus, detail.StatusLabel,
		detail.Cost, detail.ROI, detail.ViewNum, detail.ClickNum, detail.FocusNum, detail.OrderNum)

	_ = c.orderRepo.AppendQueryResponse(order.ID, map[string]any{
		"type":       "cron_poll",
		"detail":     detail,
		"checked_at": time.Now().Format(time.RFC3339),
	})

	if detailJSON, err := json.Marshal(detail); err == nil {
		_ = c.orderRepo.UpdateLatestDetail(order.ID, datatypes.JSON(detailJSON))
	}

	planStatus := detail.Status
	autoClose := detail.AutoCloseExecStatus
	statusLabel := detail.StatusLabel

	var newStatus string
	var closeReason *string

	switch {
	case autoClose == 2:
		newStatus = model.OrderStatusClosed
		r := fmt.Sprintf("自动关停失败 (auto_close=2, %s)", statusLabel)
		closeReason = &r
	case autoClose == 3:
		newStatus = model.OrderStatusClosed
		r := fmt.Sprintf("计划已结束 (auto_close=3, %s)", statusLabel)
		closeReason = &r
	default:
		switch planStatus {
		case 1:
			newStatus = model.OrderStatusActive
		case 5, 10:
			newStatus = model.OrderStatusReview
		case 3:
			if order.Status == model.OrderStatusPending || order.Status == model.OrderStatusInit {
				log.Printf("service=cron action=poll_order order=%s plan_status=3 next=check_payment", sid)
				return c.checkPayment(order, promotionID)
			}
			newStatus = model.OrderStatusPending
		case 0:
			if order.Status == model.OrderStatusPending || order.Status == model.OrderStatusInit {
				log.Printf("service=cron action=poll_order order=%s plan_status=0 next=check_payment", sid)
				return c.checkPayment(order, promotionID)
			}
			newStatus = model.OrderStatusClosed
			r := fmt.Sprintf("计划已结束 (status=0, %s)", statusLabel)
			closeReason = &r
		case 6:
			newStatus = model.OrderStatusFailed
			r := fmt.Sprintf("审核不通过 (status=6, %s)", statusLabel)
			closeReason = &r
		case 2, 7, 8, 9:
			newStatus = model.OrderStatusClosed
			r := fmt.Sprintf("计划%s (status=%d, %s)", statusLabel, planStatus, statusLabel)
			closeReason = &r
		}
	}

	if newStatus != "" && newStatus != order.Status {
		c.updateOrderStatus(order.ID, order.CampaignID, newStatus, closeReason)
		log.Printf("service=cron action=update_status order=%s plan_status=%d auto_close=%d cost=%s label=%s from=%s to=%s",
			sid, planStatus, autoClose, detail.Cost, statusLabel, order.Status, newStatus)
	} else {
		log.Printf("service=cron action=poll_order order=%s plan_status=%d auto_close=%d cost=%s label=%s result=no_change status=%s",
			sid, planStatus, autoClose, detail.Cost, statusLabel, order.Status)
	}

	actualStatus := newStatus
	if actualStatus == "" {
		actualStatus = order.Status
	}
	if actualStatus == model.OrderStatusActive {
		c.fetchRecord(accountIDStr, order.ID, promotionID)
	}

	return newStatus != "" && newStatus != order.Status
}

func (c *CheckOrderService) fetchRecord(accountIDStr string, orderID int64, promotionID string) {
	sid := strconv.FormatInt(orderID, 10)
	record, err := c.weixinClient.GetPlanRecord(accountIDStr, promotionID)
	if err != nil {
		log.Printf("service=cron action=fetch_record order=%s promotion=%s error=%v", sid, promotionID, err)
		return
	}

	_ = c.orderRepo.AppendQueryResponse(orderID, map[string]any{
		"type":       "cron_record",
		"record":     record,
		"checked_at": time.Now().Format(time.RFC3339),
	})

	if recordJSON, err := json.Marshal(record); err == nil {
		_ = c.orderRepo.UpdateLatestRecord(orderID, datatypes.JSON(recordJSON))
	}

	log.Printf("service=cron action=fetch_record order=%s promotion=%s record_count=%d", sid, promotionID, len(record))
}

func (c *CheckOrderService) checkPayment(order model.Order, promotionID string) bool {
	sid := strconv.FormatInt(order.ID, 10)
	accountIDStr := strconv.FormatInt(order.AccountID, 10)
	log.Printf("service=cron action=check_payment order=%s promotion=%s status=%s", sid, promotionID, order.Status)

	statusData, fullResp, err := c.weixinClient.PollPaymentStatus(accountIDStr, promotionID)
	if err != nil {
		log.Printf("service=cron action=check_payment order=%s promotion=%s error=%v", sid, promotionID, err)
		return false
	}

	log.Printf("service=cron action=check_payment order=%s promotion=%s response=%s", sid, promotionID, string(fullResp))

	_ = c.orderRepo.AppendQueryResponse(order.ID, map[string]any{
		"type":       "cron_check_payment",
		"response":   fullResp,
		"checked_at": time.Now().Format(time.RFC3339),
	})

	if statusData == nil {
		log.Printf("service=cron action=check_payment order=%s promotion=%s result=nil_response", sid, promotionID)
		return false
	}

	payStatus := statusData.PayStatus

	switch payStatus {
	case 1:
		c.updateOrderStatus(order.ID, order.CampaignID, model.OrderStatusReview, nil)
		log.Printf("service=cron action=check_payment order=%s promotion=%s pay_status=1 from=%s to=review", sid, promotionID, order.Status)
		return true
	case 3, 4, 5:
		reason := fmt.Sprintf("支付失败 pay_status=%d", payStatus)
		c.updateOrderStatus(order.ID, order.CampaignID, model.OrderStatusFailed, &reason)
		log.Printf("service=cron action=check_payment order=%s promotion=%s pay_status=%d from=%s to=failed", sid, promotionID, payStatus, order.Status)
		return true
	default:
		log.Printf("service=cron action=check_payment order=%s promotion=%s pay_status=%d result=waiting", sid, promotionID, payStatus)
		return false
	}
}

func (c *CheckOrderService) tryFetchPlatformOrder(order model.Order) string {
	sid := strconv.FormatInt(order.ID, 10)
	accountIDStr := strconv.FormatInt(order.AccountID, 10)
	batchID := *order.BatchID

	log.Printf("service=cron action=fetch_platform_order order=%s batch=%s", sid, batchID)

	batchData, batchResp, err := c.weixinClient.GetBatchOrders(accountIDStr, batchID)
	if err != nil {
		log.Printf("service=cron action=fetch_platform_order order=%s batch=%s error=%v", sid, batchID, err)
		return ""
	}

	log.Printf("service=cron action=fetch_platform_order order=%s batch=%s response=%s", sid, batchID, string(batchResp))

	_ = c.orderRepo.AppendQueryResponse(order.ID, map[string]any{
		"type":       "cron_fetch_batch",
		"response":   batchResp,
		"checked_at": time.Now().Format(time.RFC3339),
	})

	if batchData == nil {
		log.Printf("service=cron action=fetch_platform_order order=%s batch=%s result=nil_response", sid, batchID)
		return ""
	}

	for _, item := range batchData.List {
		if item.CreateErrorMsg != "" {
			reason := fmt.Sprintf("微信平台创建失败: %s", item.CreateErrorMsg)
			c.updateOrderStatus(order.ID, order.CampaignID, model.OrderStatusFailed, &reason)
			log.Printf("service=cron action=fetch_platform_order order=%s batch=%s error=%s", sid, batchID, item.CreateErrorMsg)
			return ""
		}
	}

	promotionID := batchData.PlanPayRes.OrderID
	payURL := batchData.PlanPayRes.PayURL

	if promotionID == "" {
		log.Printf("service=cron action=fetch_platform_order order=%s batch=%s result=pending reason=no_promotion_id", sid, batchID)
		return ""
	}

	_ = c.orderRepo.UpdatePlatformID(order.ID, promotionID)
	if payURL != "" {
		_ = c.orderRepo.UpdateZhugeSubmitted(order.ID, batchID, payURL)
	}

	log.Printf("service=cron action=fetch_platform_order order=%s batch=%s promotion=%s pay_url=%s", sid, batchID, promotionID, payURL)
	return promotionID
}
