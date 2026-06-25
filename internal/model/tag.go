package model

import "time"

type ZhugeTag struct {
	ID         string    `gorm:"primaryKey;type:varchar(32)" json:"id"`
	Text       string    `gorm:"type:varchar(128);not null" json:"text"`
	ParentID   *string   `gorm:"type:varchar(32)" json:"parent_id"`
	ParentText *string   `gorm:"type:varchar(128)" json:"parent_text"`
	WxLevel    int       `gorm:"column:wx_level;default:2" json:"wx_level"`
	UpdatedAt  time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (ZhugeTag) TableName() string { return "zhuge_tags" }

// TagMeta 标签元信息（用于 flatTags 传递 ID + 平台 level）
type TagMeta struct {
	ID      string
	WxLevel int
}

// ZhugeTagCategory 标签与一级分类的多对多关联
type ZhugeTagCategory struct {
	TagID      string `gorm:"primaryKey;type:varchar(32)" json:"tag_id"`
	CategoryID string `gorm:"primaryKey;type:varchar(32)" json:"category_id"`
}

func (ZhugeTagCategory) TableName() string { return "zhuge_tag_categories" }
