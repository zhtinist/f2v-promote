package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type FeishuSheetTabRepo struct {
	db *gorm.DB
}

func NewFeishuSheetTabRepo(db *gorm.DB) *FeishuSheetTabRepo {
	return &FeishuSheetTabRepo{db: db}
}

// GetByExportID 查找指定 (author_id, month, export_id) 对应的 sheet tab
func (r *FeishuSheetTabRepo) GetByExportID(authorID int64, month, exportID string) (*model.FeishuSheetTab, error) {
	var tab model.FeishuSheetTab
	err := r.db.Where("author_id = ? AND month = ? AND export_id = ?", authorID, month, exportID).First(&tab).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &tab, nil
}

// CountByPublishDate 统计同作者同月同发布日期已有的 sheet tab 数量（用于命名后缀）
func (r *FeishuSheetTabRepo) CountByPublishDate(authorID int64, month, publishDate string) (int64, error) {
	var count int64
	err := r.db.Model(&model.FeishuSheetTab{}).
		Where("author_id = ? AND month = ? AND publish_date = ?", authorID, month, publishDate).
		Count(&count).Error
	return count, err
}

// ListByAuthorMonth 获取某作者某月下所有已分配的 sheet tab
func (r *FeishuSheetTabRepo) ListByAuthorMonth(authorID int64, month string) ([]model.FeishuSheetTab, error) {
	var list []model.FeishuSheetTab
	err := r.db.Where("author_id = ? AND month = ?", authorID, month).Order("sheet_title ASC").Find(&list).Error
	return list, err
}

// Create 创建映射记录
func (r *FeishuSheetTabRepo) Create(tab *model.FeishuSheetTab) error {
	return r.db.Create(tab).Error
}

// DeleteBySpreadsheetToken 删除某电子表格下所有 tab 记录（用于重建场景）
func (r *FeishuSheetTabRepo) DeleteBySpreadsheetToken(spreadsheetToken string) error {
	return r.db.Where("spreadsheet_token = ?", spreadsheetToken).Delete(&model.FeishuSheetTab{}).Error
}
