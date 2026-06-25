package model

type User struct {
	Base
	Username       string  `gorm:"uniqueIndex;type:varchar(64);not null" json:"username"`
	HashedPassword string  `gorm:"type:varchar(256);not null" json:"-"`
	CostThreshold  float64 `gorm:"type:decimal(6,2);default:1.0" json:"cost_threshold"`
}

func (User) TableName() string { return "users" }
