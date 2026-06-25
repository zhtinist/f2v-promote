package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type FeishuFolderRepo struct {
	db *gorm.DB
}

func NewFeishuFolderRepo(db *gorm.DB) *FeishuFolderRepo {
	return &FeishuFolderRepo{db: db}
}

// GetAuthorFolder 获取作者文件夹
func (r *FeishuFolderRepo) GetAuthorFolder(authorID int64) (*model.FeishuFolder, error) {
	var f model.FeishuFolder
	if err := r.db.Where("author_id = ? AND folder_type = 'author'", authorID).First(&f).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

// GetMonthFolder 获取作者某月的文件夹
func (r *FeishuFolderRepo) GetMonthFolder(authorID int64, month string) (*model.FeishuFolder, error) {
	var f model.FeishuFolder
	if err := r.db.Where("author_id = ? AND folder_type = 'month' AND month = ?", authorID, month).First(&f).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

// GetPlatformFolder 获取作者某月某平台的文件夹
func (r *FeishuFolderRepo) GetPlatformFolder(authorID int64, month, platform string) (*model.FeishuFolder, error) {
	var f model.FeishuFolder
	if err := r.db.Where("author_id = ? AND folder_type = 'platform' AND month = ? AND platform = ?", authorID, month, platform).First(&f).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &f, nil
}

// Create 新建文件夹记录
func (r *FeishuFolderRepo) Create(f *model.FeishuFolder) error {
	return r.db.Create(f).Error
}
