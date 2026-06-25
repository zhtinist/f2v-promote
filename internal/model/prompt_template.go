package model

type PromptTemplate struct {
	Base
	Name        string  `gorm:"uniqueIndex;type:varchar(64);not null" json:"name"`
	Title       string  `gorm:"type:varchar(128);not null" json:"title"`
	Content     string  `gorm:"type:text;not null" json:"content"`
	Description *string `gorm:"type:text" json:"description"`
}

func (PromptTemplate) TableName() string { return "prompt_templates" }
