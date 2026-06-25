package model

import (
	"time"

	"gorm.io/datatypes"
)

// 状态常量
const (
	PromoteLogDetected  = "detected"  // 检测命中，待审核（notify_feishu=true）或自动确认
	PromoteLogConfirmed = "confirmed" // 已确认，等待 Dispatcher 推 MNS
	PromoteLogRejected  = "rejected"  // 人工拒绝（终态）
	PromoteLogQueued    = "queued"    // 已推 MNS，等待 Consumer 消费
	PromoteLogRunning   = "running"   // Consumer 执行中
	PromoteLogCompleted = "completed" // 下单成功
	PromoteLogFailed    = "failed"    // 下单失败
)

// AutoPromoteLog 自动投放执行日志
type AutoPromoteLog struct {
	Base
	StrategyID      int64          `gorm:"not null;index" json:"strategy_id"`
	AuthorID        int64          `gorm:"not null" json:"author_id"`
	AccountID       int64          `gorm:"not null" json:"account_id"`
	Platform        string         `gorm:"type:varchar(32);not null;default:'weixin'" json:"platform"`
	AuthorVideoID   int64          `gorm:"not null" json:"author_video_id"`
	PromoteType     string         `gorm:"type:varchar(20);not null" json:"promote_type"`
	StatIDCurrent   int64          `gorm:"not null;uniqueIndex" json:"stat_id_current"`
	StatIDPrevious  *int64         `json:"stat_id_previous"`
	StatRawData     datatypes.JSON `gorm:"type:json" json:"stat_raw_data"`       // 前后两条 video_stat 原始数据
	HourlyPlayCount *int           `json:"hourly_play_count"`
	LikeRate        *float64       `gorm:"type:decimal(6,3)" json:"like_rate"`
	ShareRate       *float64       `gorm:"type:decimal(6,3)" json:"share_rate"`
	CampaignID      *int64         `json:"campaign_id"`
	OrderID         *int64         `json:"order_id"`
	Status          string         `gorm:"type:varchar(20);not null;default:'detected';index" json:"status"`
	ConfirmedAt     *time.Time     `gorm:"type:datetime" json:"confirmed_at"`
	QueuedAt        *time.Time     `gorm:"type:datetime" json:"queued_at"`
	ErrorMsg        *string        `gorm:"type:text" json:"error_msg"`
	VideoRawData    datatypes.JSON `gorm:"type:json" json:"video_raw_data"`
	AuthorRawData   datatypes.JSON `gorm:"type:json" json:"author_raw_data"`
	TagGroups       datatypes.JSON `gorm:"type:json" json:"tag_groups"`
}

func (AutoPromoteLog) TableName() string { return "auto_promote_logs" }
