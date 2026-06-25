package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type PlatformAccountRepo struct {
	db *gorm.DB
}

func NewPlatformAccountRepo(db *gorm.DB) *PlatformAccountRepo {
	return &PlatformAccountRepo{db: db}
}

func (r *PlatformAccountRepo) List() ([]model.PlatformAccount, error) {
	var list []model.PlatformAccount
	err := r.db.Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *PlatformAccountRepo) ListPaged(page, pageSize int) ([]model.PlatformAccount, int64, error) {
	var total int64
	r.db.Model(&model.PlatformAccount{}).Count(&total)
	var list []model.PlatformAccount
	err := r.db.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *PlatformAccountRepo) GetByID(id int64) (*model.PlatformAccount, error) {
	var a model.PlatformAccount
	if err := r.db.Where("id = ?", id).First(&a).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &a, nil
}

func (r *PlatformAccountRepo) Create(a *model.PlatformAccount) error {
	return r.db.Create(a).Error
}

func (r *PlatformAccountRepo) Update(id int64, updates map[string]any) error {
	return r.db.Model(&model.PlatformAccount{}).Where("id = ?", id).Updates(updates).Error
}

func (r *PlatformAccountRepo) Delete(id int64) error {
	return r.db.Where("id = ?", id).Delete(&model.PlatformAccount{}).Error
}

// UpdateAccountConfig 更新 JSON 配置（如 token 刷新后回写）
func (r *PlatformAccountRepo) UpdateAccountConfig(id int64, configJSON []byte) error {
	return r.db.Model(&model.PlatformAccount{}).Where("id = ?", id).
		Update("account_config", configJSON).Error
}

func (r *PlatformAccountRepo) GetActiveByPlatform(platform string) ([]model.PlatformAccount, error) {
	var list []model.PlatformAccount
	err := r.db.Where("status = ? AND platform = ?", "active", platform).Find(&list).Error
	return list, err
}
