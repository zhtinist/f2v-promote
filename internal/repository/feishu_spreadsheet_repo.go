package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type FeishuSpreadsheetRepo struct {
	db *gorm.DB
}

func NewFeishuSpreadsheetRepo(db *gorm.DB) *FeishuSpreadsheetRepo {
	return &FeishuSpreadsheetRepo{db: db}
}

// GetByAuthorMonthPlatform 查询作者某月某平台的电子表格记录
func (r *FeishuSpreadsheetRepo) GetByAuthorMonthPlatform(authorID int64, month, platform string) (*model.FeishuSpreadsheet, error) {
	var ss model.FeishuSpreadsheet
	if err := r.db.Where("author_id = ? AND month = ? AND platform = ?", authorID, month, platform).First(&ss).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &ss, nil
}

// Create 新建电子表格记录
func (r *FeishuSpreadsheetRepo) Create(ss *model.FeishuSpreadsheet) error {
	return r.db.Create(ss).Error
}

// Delete 按 ID 删除电子表格记录
func (r *FeishuSpreadsheetRepo) Delete(id int64) error {
	return r.db.Where("id = ?", id).Delete(&model.FeishuSpreadsheet{}).Error
}
