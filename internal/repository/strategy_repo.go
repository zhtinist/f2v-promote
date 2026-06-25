package repository

import (
	"time"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type StrategyRepo struct {
	db *gorm.DB
}

func NewStrategyRepo(db *gorm.DB) *StrategyRepo {
	return &StrategyRepo{db: db}
}

// ListEnabled 获取所有启用的策略
func (r *StrategyRepo) ListEnabled() ([]model.AuthorPromoteStrategy, error) {
	var list []model.AuthorPromoteStrategy
	err := r.db.Debug().Where("enabled = ?", true).Find(&list).Error
	return list, err
}

// List 获取全部策略
func (r *StrategyRepo) List() ([]model.AuthorPromoteStrategy, error) {
	var list []model.AuthorPromoteStrategy
	err := r.db.Order("created_at DESC").Find(&list).Error
	return list, err
}

// GetByID 按 ID 查询
func (r *StrategyRepo) GetByID(id int64) (*model.AuthorPromoteStrategy, error) {
	var s model.AuthorPromoteStrategy
	if err := r.db.Where("id = ?", id).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// GetByAuthorID 按作者 ID 查询
func (r *StrategyRepo) GetByAuthorID(authorID int64) (*model.AuthorPromoteStrategy, error) {
	var s model.AuthorPromoteStrategy
	if err := r.db.Where("author_id = ?", authorID).First(&s).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &s, nil
}

// Create 创建策略
func (r *StrategyRepo) Create(s *model.AuthorPromoteStrategy) error {
	return r.db.Create(s).Error
}

// Update 更新策略
func (r *StrategyRepo) Update(s *model.AuthorPromoteStrategy) error {
	return r.db.Save(s).Error
}

// Delete 删除策略
func (r *StrategyRepo) Delete(id int64) error {
	return r.db.Where("id = ?", id).Delete(&model.AuthorPromoteStrategy{}).Error
}

// TouchLastChecked 更新水位线为指定的 collect_date 时间
func (r *StrategyRepo) TouchLastChecked(id int64, checkedAt time.Time) error {
	return r.db.Model(&model.AuthorPromoteStrategy{}).
		Where("id = ?", id).
		Update("last_checked_at", checkedAt).Error
}

// CountEnabled 启用中的策略数
func (r *StrategyRepo) CountEnabled() (int64, error) {
	var count int64
	err := r.db.Model(&model.AuthorPromoteStrategy{}).Where("enabled = ?", true).Count(&count).Error
	return count, err
}
