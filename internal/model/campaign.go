package model

import (
	"time"

	"gorm.io/datatypes"
)

type Campaign struct {
	Base
	UserID     int64          `gorm:"not null" json:"user_id"`
	AccountID  int64          `json:"account_id"`
	Script     string         `gorm:"type:text;not null" json:"script"`
	TagGroups  datatypes.JSON `gorm:"type:json" json:"tag_groups"`
	Status     string         `gorm:"type:varchar(20);default:'pending'" json:"status"`
	ErrorMsg   *string        `gorm:"type:text" json:"error_msg"`
	FinishedAt *time.Time     `gorm:"column:finished_at" json:"finished_at"`
}

func (Campaign) TableName() string { return "campaigns" }
