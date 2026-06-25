package model

type PerformanceLog struct {
	Base
	OrderID         int64    `gorm:"not null" json:"order_id"`
	Spend           float64  `gorm:"type:decimal(10,2);not null" json:"spend"`
	Followers       int      `gorm:"not null" json:"followers"`
	CostPerFollower *float64 `gorm:"type:decimal(8,4)" json:"cost_per_follower"`
}

func (PerformanceLog) TableName() string { return "performance_logs" }
