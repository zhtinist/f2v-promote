package model

import (
	"time"

	"gorm.io/datatypes"
)

// Author 作者（替代原 ZhugeAuthor）
type Author struct {
	Base
	AccountID         int64          `gorm:"not null;index" json:"account_id"` // 关联 platform_accounts.id
	Username          string         `gorm:"type:varchar(256);uniqueIndex" json:"username"`
	Nickname          string         `gorm:"type:varchar(256)" json:"nickname"`
	AvatarURL         string         `gorm:"type:text" json:"avatar_url"`
	Platform          string         `gorm:"type:varchar(32);default:'weixin';index" json:"platform"`
	RawData           datatypes.JSON `gorm:"type:json" json:"raw_data"`
	FeishuSyncEnabled bool           `gorm:"default:false" json:"feishu_sync_enabled"`
	FeishuFolderToken *string        `gorm:"type:varchar(128)" json:"feishu_folder_token"`
	CachedAt          time.Time      `gorm:"type:datetime" json:"cached_at"`
}

func (Author) TableName() string { return "authors" }
