package model

import "time"

// AuthorPromoteStrategy 作者投放策略配置
type AuthorPromoteStrategy struct {
	Base
	AuthorID                int64      `gorm:"not null;uniqueIndex" json:"author_id"`
	AccountID               int64      `gorm:"not null" json:"account_id"`
	Platform                string     `gorm:"type:varchar(32);not null;default:'weixin'" json:"platform"`
	Enabled                 bool       `gorm:"default:true" json:"enabled"`
	NotifyFeishu            bool       `gorm:"default:false" json:"notify_feishu"`
	HourlyPlayThreshold     int        `gorm:"not null" json:"hourly_play_threshold"`
	LikeRateThreshold       float64    `gorm:"type:decimal(6,3);default:0" json:"like_rate_threshold"`
	CommentRateThreshold    float64    `gorm:"type:decimal(6,3);default:0" json:"comment_rate_threshold"`
	FollowRateThreshold     float64    `gorm:"type:decimal(6,3);default:0" json:"follow_rate_threshold"`
	ShareRateThreshold      float64    `gorm:"type:decimal(6,3);default:0" json:"share_rate_threshold"`
	CompletionRateThreshold float64    `gorm:"type:decimal(6,3);default:0" json:"completion_rate_threshold"`
	StopCoefficient         float64    `gorm:"type:decimal(6,3);not null;default:0.7" json:"stop_coefficient"`
	StopDecayCoefficient    float64    `gorm:"type:decimal(6,3);not null;default:0.8" json:"stop_decay_coefficient"`
	FanCostThreshold        float64    `gorm:"type:decimal(6,2);not null;default:0.80" json:"fan_cost_threshold"`
	StopProtectionMin       int        `gorm:"not null;default:30" json:"stop_protection_min"`
	LastCheckedAt           *time.Time `gorm:"type:datetime" json:"last_checked_at"`
}

func (AuthorPromoteStrategy) TableName() string { return "author_promote_strategies" }
