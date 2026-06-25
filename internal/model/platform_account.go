package model

import "gorm.io/datatypes"

// PlatformAccount 平台账号配置（替代原 ZhugeAccount）
// AccountConfig 为 JSON 字段，按 Platform 不同存储不同的凭证信息
type PlatformAccount struct {
	Base
	Name          string         `gorm:"type:varchar(64);not null" json:"name"`
	Platform      string         `gorm:"type:varchar(32);not null;default:'weixin';index" json:"platform"`
	AccountConfig datatypes.JSON `gorm:"type:json;not null" json:"account_config"` // 平台特定配置 JSON
	Status        string         `gorm:"type:varchar(20);default:'active'" json:"status"`
}

func (PlatformAccount) TableName() string { return "platform_accounts" }

// ── 各平台 AccountConfig 结构 ──

// WeixinAccountConfig 微信平台（诸葛）配置
type WeixinAccountConfig struct {
	Account        string `json:"account"`
	Password       string `json:"password"`
	TsUserID       string `json:"ts_user_id"`
	GroupID        string `json:"group_id,omitempty"`
	Token          string `json:"token,omitempty"`
	TokenExpiresAt string `json:"token_expires_at,omitempty"` // RFC3339
}
