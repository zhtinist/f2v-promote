package model

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

// AuthorVideo 作者视频关联表，建立 export_id ↔ author_id 映射
type AuthorVideo struct {
	Base
	AuthorID      int64          `gorm:"not null;index" json:"author_id"`
	AccountID     int64          `gorm:"not null;index" json:"account_id"`
	ExportID      string         `gorm:"type:varchar(256);not null;uniqueIndex" json:"export_id"`
	Description   string         `gorm:"type:text" json:"description"`
	CoverURL      string         `gorm:"type:text" json:"cover_url"`
	PublishTime   string         `gorm:"type:varchar(32)" json:"publish_time"`
	Nonce         string         `gorm:"type:varchar(256);not null" json:"nonce"`
	RawData       datatypes.JSON `gorm:"type:json" json:"raw_data"`
	LastCheckedAt *time.Time     `gorm:"type:datetime" json:"last_checked_at"`
}

func (AuthorVideo) TableName() string { return "author_videos" }

// GetCreateTime 从 raw_data 中解析 createTime（unix timestamp）得到第三方发布时间
func (av *AuthorVideo) GetCreateTime() *time.Time {
	if len(av.RawData) == 0 {
		return nil
	}
	var raw struct {
		CreateTime int64 `json:"createTime"`
	}
	if err := json.Unmarshal(av.RawData, &raw); err != nil || raw.CreateTime == 0 {
		return nil
	}
	t := time.Unix(raw.CreateTime, 0).Add(8 * time.Hour)
	return &t
}
