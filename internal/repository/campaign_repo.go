package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type CampaignRepo struct {
	db *gorm.DB
}

func NewCampaignRepo(db *gorm.DB) *CampaignRepo {
	return &CampaignRepo{db: db}
}

// Create inserts a new campaign.
func (r *CampaignRepo) Create(userID, accountID int64, script string, tagGroups datatypes.JSON) (*model.Campaign, error) {
	c := model.Campaign{
		UserID:    userID,
		AccountID: accountID,
		Script:    script,
		TagGroups: tagGroups,
		Status:    "pending",
	}
	if err := r.db.Create(&c).Error; err != nil {
		return nil, err
	}
	return &c, nil
}

// GetByID returns a campaign by primary key.
func (r *CampaignRepo) GetByID(id int64) (*model.Campaign, error) {
	var c model.Campaign
	if err := r.db.Where("id = ?", id).First(&c).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &c, nil
}

// GetTags returns just the tag_groups JSON for a campaign.
func (r *CampaignRepo) GetTags(id int64) (datatypes.JSON, error) {
	var c model.Campaign
	if err := r.db.Select("tag_groups").Where("id = ?", id).First(&c).Error; err != nil {
		return nil, err
	}
	return c.TagGroups, nil
}

// GetByUserID returns all campaigns for a user, ordered by created_at desc.
func (r *CampaignRepo) GetByUserID(userID int64) ([]model.Campaign, error) {
	var list []model.Campaign
	err := r.db.Where("user_id = ?", userID).Order("created_at DESC").Find(&list).Error
	return list, err
}

// CountByUserID returns the count of campaigns for a user.
func (r *CampaignRepo) CountByUserID(userID int64) (int64, error) {
	var count int64
	err := r.db.Model(&model.Campaign{}).Where("user_id = ?", userID).Count(&count).Error
	return count, err
}

// PaginateByUserID 分页查询用户活动，支持状态筛选
func (r *CampaignRepo) PaginateByUserID(userID int64, status string, filterID int64, startDate, endDate string, page, pageSize int) ([]model.Campaign, int64, error) {
	q := r.db.Model(&model.Campaign{}).Where("user_id = ?", userID)
	if status != "" {
		q = q.Where("status = ?", status)
	}
	if filterID > 0 {
		q = q.Where("id = ?", filterID)
	}
	if startDate != "" {
		q = q.Where("created_at >= ?", startDate+" 00:00:00")
	}
	if endDate != "" {
		q = q.Where("created_at <= ?", endDate+" 23:59:59")
	}
	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var list []model.Campaign
	err := q.Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// UpdateStatus sets the status (and optionally error_msg) for a campaign.
func (r *CampaignRepo) UpdateStatus(id int64, status string, errorMsg *string) error {
	updates := map[string]interface{}{
		"status": status,
	}
	if errorMsg != nil {
		updates["error_msg"] = *errorMsg
	}
	return r.db.Model(&model.Campaign{}).Where("id = ?", id).Updates(updates).Error
}
