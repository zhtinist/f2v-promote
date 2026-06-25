package model

import "time"

// FeishuSyncCursor 飞书同步游标
type FeishuSyncCursor struct {
	Base
	AuthorID     int64     `gorm:"not null;uniqueIndex" json:"author_id"`
	LastSyncedAt time.Time `gorm:"type:datetime;not null" json:"last_synced_at"`
	SyncCount    int       `gorm:"default:0" json:"sync_count"`
}

func (FeishuSyncCursor) TableName() string { return "feishu_sync_cursors" }
