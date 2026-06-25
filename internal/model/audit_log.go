package model

import "gorm.io/datatypes"

type AuditLog struct {
	Base
	UserID *int64         `json:"user_id"`
	Action string         `gorm:"type:varchar(128);not null" json:"action"`
	Detail datatypes.JSON `gorm:"type:json" json:"detail"`
}

func (AuditLog) TableName() string { return "audit_logs" }
