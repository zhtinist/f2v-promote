package model

// FeishuSheetTab 飞书电子表格中视频与 sheet 页的映射（每个视频一个 sheet）
type FeishuSheetTab struct {
	Base
	AuthorID         int64  `gorm:"not null;index:idx_sheet_tab_lookup" json:"author_id"`
	Month            string `gorm:"type:varchar(7);not null;index:idx_sheet_tab_lookup" json:"month"`    // "2026-04"
	PublishDate      string `gorm:"type:varchar(10);not null" json:"publish_date"`                       // "2026-04-03"
	ExportID         string `gorm:"type:varchar(256);not null;index:idx_sheet_tab_lookup" json:"export_id"` // 视频 export_id
	SpreadsheetToken string `gorm:"type:varchar(128);not null" json:"spreadsheet_token"`
	SheetID          string `gorm:"type:varchar(64);not null" json:"sheet_id"`    // 飞书 sheet tab ID
	SheetTitle       string `gorm:"type:varchar(32);not null" json:"sheet_title"` // "04-03A"
}

func (FeishuSheetTab) TableName() string { return "feishu_sheet_tabs" }
