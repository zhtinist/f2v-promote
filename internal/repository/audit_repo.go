package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type AuditRepo struct {
	db *gorm.DB
}

func NewAuditRepo(db *gorm.DB) *AuditRepo {
	return &AuditRepo{db: db}
}

// Add creates a new audit log entry.
func (r *AuditRepo) Add(userID *int64, action string, detail datatypes.JSON) error {
	log := model.AuditLog{
		UserID: userID,
		Action: action,
		Detail: detail,
	}
	return r.db.Create(&log).Error
}
