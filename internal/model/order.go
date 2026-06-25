package model

import (
	"time"

	"gorm.io/datatypes"
)

// 订单状态常量
const (
	OrderStatusInit    = "init"    // 初始化，还没调诸葛
	OrderStatusPending = "pending" // 调诸葛创建成功，等待支付
	OrderStatusReview  = "review"  // 已支付，审核中
	OrderStatusActive  = "active"  // 加热中
	OrderStatusFailed  = "failed"  // 订单创建失败
	OrderStatusClosed  = "closed"  // 订单关闭
)

// IsTerminalStatus 是否为终态（不再轮询）
func IsTerminalStatus(s string) bool {
	return s == OrderStatusFailed || s == OrderStatusClosed
}

type Order struct {
	Base
	CampaignID      int64          `gorm:"not null" json:"campaign_id"`
	UserID          int64          `gorm:"not null;index" json:"user_id"`
	AccountID       int64          `json:"account_id"`
	TagGroup        datatypes.JSON `gorm:"type:json;not null" json:"tag_group"`
	PlatformOrderID *string        `gorm:"type:varchar(128)" json:"platform_order_id"`
	Status          string         `gorm:"type:varchar(20);default:'init'" json:"status"`
	CloseReason     *string        `gorm:"type:text" json:"close_reason"`
	BatchID         *string        `gorm:"type:varchar(64)" json:"batch_id"`
	PayURL          *string        `gorm:"type:text" json:"pay_url"`
	Source          string         `gorm:"type:varchar(20);default:'weixin'" json:"source"`
	CreateRequest   datatypes.JSON `gorm:"type:json" json:"create_request"`
	CreateResponse  datatypes.JSON `gorm:"type:json" json:"create_response"`
	QueryResponse   datatypes.JSON `gorm:"type:json" json:"query_response"`
	LatestDetail    datatypes.JSON `gorm:"type:json" json:"latest_detail"`
	LatestRecord    datatypes.JSON `gorm:"type:json" json:"latest_record"`
	QueryAt         *time.Time     `gorm:"type:datetime" json:"query_at"`
}

func (Order) TableName() string { return "orders" }
