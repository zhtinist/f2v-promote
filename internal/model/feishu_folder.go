package model

// FeishuFolder 飞书文件夹记录
// folder_type: author / month / platform
type FeishuFolder struct {
	Base
	AuthorID    int64  `gorm:"not null;index" json:"author_id"`
	FolderType  string `gorm:"type:varchar(16);not null" json:"folder_type"`
	Month       string `gorm:"type:varchar(7)" json:"month"`
	Platform    string `gorm:"type:varchar(32);default:''" json:"platform"` // folder_type=platform 时填充
	FolderToken string `gorm:"type:varchar(128);not null" json:"folder_token"`
	Name        string `gorm:"type:varchar(256);not null" json:"name"`
}

func (FeishuFolder) TableName() string { return "feishu_folders" }
