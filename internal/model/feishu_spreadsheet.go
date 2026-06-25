package model

// FeishuSpreadsheet 飞书电子表格注册表（按月粒度）
type FeishuSpreadsheet struct {
	Base
	AuthorID         int64  `gorm:"not null;index" json:"author_id"`
	Platform         string `gorm:"type:varchar(32);default:'weixin'" json:"platform"` // 平台标识
	Month            string `gorm:"type:varchar(7)" json:"month"`                      // "2026-04"
	SpreadsheetToken string `gorm:"type:varchar(128);not null" json:"spreadsheet_token"`
	Title            string `gorm:"type:varchar(256);not null" json:"title"`
}

func (FeishuSpreadsheet) TableName() string { return "feishu_spreadsheets" }
