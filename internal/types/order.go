package types

import (
	"encoding/json"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/center/weixin"
)

// ── 响应类型 ──

// SubmitOrderResult 提交订单的返回
type SubmitOrderResult struct {
	OrderID         int64  `json:"order_id"`
	Status          string `json:"status"`
	BatchID         string `json:"batch_id,omitempty"`
	PlatformOrderID string `json:"platform_order_id,omitempty"`
	PayURL          string `json:"pay_url,omitempty"`
	Error           string `json:"error,omitempty"`
}

// CheckPaymentResult 查询支付状态的返回
type CheckPaymentResult struct {
	OrderID         int64   `json:"order_id"`
	Status          string  `json:"status"`
	Paid            bool    `json:"paid"`
	PlatformOrderID *string `json:"platform_order_id,omitempty"`
	PayURL          *string `json:"pay_url,omitempty"`
	CloseReason     *string `json:"close_reason,omitempty"`
}

// ConfirmOrderItem 确认投放后返回的订单项
type ConfirmOrderItem struct {
	ID       int64           `json:"id"`
	TagGroup json.RawMessage `json:"tag_group"`
	Status   string          `json:"status"`
}

// OrderDetailResult 订单详情返回
type OrderDetailResult struct {
	OrderID int64              `json:"order_id"`
	Detail  *weixin.PlanDetail `json:"detail"`
	QueryAt *time.Time         `json:"query_at,omitempty"`
	Msg     string             `json:"msg,omitempty"`
}

// OrderRecordResult 订单操作日志返回
type OrderRecordResult struct {
	OrderID int64               `json:"order_id"`
	Record  []weixin.PlanRecord `json:"record"`
	QueryAt *time.Time          `json:"query_at,omitempty"`
	Msg     string              `json:"msg,omitempty"`
}
