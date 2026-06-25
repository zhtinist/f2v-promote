package repository

import (
	"time"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type AutoPromoteLogRepo struct {
	db *gorm.DB
}

func NewAutoPromoteLogRepo(db *gorm.DB) *AutoPromoteLogRepo {
	return &AutoPromoteLogRepo{db: db}
}

// Create 创建日志（状态 detected 或 confirmed）
func (r *AutoPromoteLogRepo) Create(log *model.AutoPromoteLog) error {
	return r.db.Create(log).Error
}

// GetByID 按 ID 查询
func (r *AutoPromoteLogRepo) GetByID(id int64) (*model.AutoPromoteLog, error) {
	var log model.AutoPromoteLog
	if err := r.db.Where("id = ?", id).First(&log).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &log, nil
}

// UpdateStatus 更新状态 + 关联字段
func (r *AutoPromoteLogRepo) UpdateStatus(id int64, status string, updates map[string]any) error {
	if updates == nil {
		updates = make(map[string]any)
	}
	updates["status"] = status
	return r.db.Model(&model.AutoPromoteLog{}).Where("id = ?", id).Updates(updates).Error
}

// ConfirmByID 人工确认：detected → confirmed（原子 CAS）
func (r *AutoPromoteLogRepo) ConfirmByID(id int64) (bool, error) {
	now := time.Now()
	result := r.db.Model(&model.AutoPromoteLog{}).
		Where("id = ? AND status = ?", id, model.PromoteLogDetected).
		Updates(map[string]any{
			"status":       model.PromoteLogConfirmed,
			"confirmed_at": now,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// RejectByID 人工拒绝：detected → rejected（原子 CAS）
func (r *AutoPromoteLogRepo) RejectByID(id int64) (bool, error) {
	result := r.db.Model(&model.AutoPromoteLog{}).
		Where("id = ? AND status = ?", id, model.PromoteLogDetected).
		Updates(map[string]any{
			"status": model.PromoteLogRejected,
		})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}

// MarkAsQueued Dispatcher 原子更新：confirmed → queued + set queued_at（返回影响行数）
func (r *AutoPromoteLogRepo) MarkAsQueued(id int64) (int64, error) {
	now := time.Now()
	result := r.db.Model(&model.AutoPromoteLog{}).
		Where("id = ? AND status = ?", id, model.PromoteLogConfirmed).
		Updates(map[string]any{
			"status":    model.PromoteLogQueued,
			"queued_at": now,
		})
	return result.RowsAffected, result.Error
}

// MarkAsRunning Consumer 原子更新：queued → running（返回影响行数，0=已消费）
func (r *AutoPromoteLogRepo) MarkAsRunning(id int64) (int64, error) {
	result := r.db.Model(&model.AutoPromoteLog{}).
		Where("id = ? AND status = ?", id, model.PromoteLogQueued).
		Updates(map[string]any{
			"status": model.PromoteLogRunning,
		})
	return result.RowsAffected, result.Error
}

// ListConfirmed 查询所有 status=confirmed 的 log（Dispatcher 扫描用，限制批次大小）
func (r *AutoPromoteLogRepo) ListConfirmed(limit int) ([]model.AutoPromoteLog, error) {
	var list []model.AutoPromoteLog
	err := r.db.Debug().Where("status = ?", model.PromoteLogConfirmed).
		Order("created_at ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// ListDetected 查询所有 status=detected 的 log（前端待审核列表）
func (r *AutoPromoteLogRepo) ListDetected(page, pageSize int) ([]model.AutoPromoteLog, int64, error) {
	var list []model.AutoPromoteLog
	var total int64

	q := r.db.Model(&model.AutoPromoteLog{}).Where("status = ?", model.PromoteLogDetected)
	q.Count(&total)

	offset := (page - 1) * pageSize
	err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// ExistsByStatIDCurrent 检查某 stat_id_current 是否已存在记录（并发去重核心）
func (r *AutoPromoteLogRepo) ExistsByStatIDCurrent(statIDCurrent int64) (bool, error) {
	var count int64
	err := r.db.Model(&model.AutoPromoteLog{}).
		Where("stat_id_current = ?", statIDCurrent).
		Count(&count).Error
	return count > 0, err
}

// HasRecentPromote 检查某视频在 cooldown 时间窗口内是否已有投放记录（任意状态）
func (r *AutoPromoteLogRepo) HasRecentPromote(authorVideoID int64, cooldown time.Duration) (bool, error) {
	var count int64
	since := time.Now().Add(-cooldown)
	err := r.db.Model(&model.AutoPromoteLog{}).
		Where("author_video_id = ? AND created_at >= ?", authorVideoID, since).
		Count(&count).Error
	return count > 0, err
}

// ListByStrategyID 按策略ID查询日志（分页）
func (r *AutoPromoteLogRepo) ListByStrategyID(strategyID int64, page, pageSize int) ([]model.AutoPromoteLog, int64, error) {
	var list []model.AutoPromoteLog
	var total int64

	q := r.db.Model(&model.AutoPromoteLog{}).Where("strategy_id = ?", strategyID)
	q.Count(&total)

	offset := (page - 1) * pageSize
	err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// ListAllLogs 全局日志列表（分页 + 筛选）
func (r *AutoPromoteLogRepo) ListAllLogs(page, pageSize int, authorID int64, authorVideoIDs []int64, status string, filterID int64, startDate, endDate string) ([]model.AutoPromoteLog, int64, error) {
	var list []model.AutoPromoteLog
	var total int64

	q := r.db.Model(&model.AutoPromoteLog{})
	if authorID > 0 {
		q = q.Where("author_id = ?", authorID)
	}
	if len(authorVideoIDs) > 0 {
		q = q.Where("author_video_id IN ?", authorVideoIDs)
	}
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
	q.Count(&total)

	offset := (page - 1) * pageSize
	err := q.Order("created_at DESC").Offset(offset).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// ListForStopEvaluation 查询需要关停评估的日志
// 条件：status=completed，有 order_id，关联订单为 active 且有 platform_order_id
func (r *AutoPromoteLogRepo) ListForStopEvaluation(limit int) ([]model.AutoPromoteLog, error) {
	var list []model.AutoPromoteLog
	err := r.db.Debug().
		Joins("JOIN orders ON orders.id = auto_promote_logs.order_id").
		Where("auto_promote_logs.status = ?", model.PromoteLogCompleted).
		Where("auto_promote_logs.order_id IS NOT NULL").
		Where("orders.status = ?", model.OrderStatusActive).
		Where("orders.platform_order_id IS NOT NULL AND orders.platform_order_id != ''").
		Order("auto_promote_logs.created_at ASC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

// TodayCount 今日自动投放数
func (r *AutoPromoteLogRepo) TodayCount() (int64, error) {
	var count int64
	err := r.db.Model(&model.AutoPromoteLog{}).
		Where("DATE(created_at) = CURDATE()").
		Count(&count).Error
	return count, err
}

// PendingCount 待审核投放数
func (r *AutoPromoteLogRepo) PendingCount() (int64, error) {
	var count int64
	err := r.db.Model(&model.AutoPromoteLog{}).
		Where("status = ?", model.PromoteLogDetected).
		Count(&count).Error
	return count, err
}

// UpdateStatusByOrderID 按订单 ID 更新投放日志状态
func (r *AutoPromoteLogRepo) UpdateStatusByOrderID(orderID int64, status string, errorMsg *string) error {
	updates := map[string]any{"status": status}
	if errorMsg != nil {
		updates["error_msg"] = *errorMsg
	}
	return r.db.Model(&model.AutoPromoteLog{}).
		Where("order_id = ?", orderID).
		Updates(updates).Error
}

// GetLogIDsByOrderIDs 批量按 order_id 查询对应的 promote_log ID
func (r *AutoPromoteLogRepo) GetLogIDsByOrderIDs(orderIDs []int64) (map[int64]int64, error) {
	if len(orderIDs) == 0 {
		return nil, nil
	}
	type result struct {
		OrderID int64 `gorm:"column:order_id"`
		ID      int64 `gorm:"column:id"`
	}
	var rows []result
	err := r.db.Model(&model.AutoPromoteLog{}).
		Select("order_id, id").
		Where("order_id IN ?", orderIDs).
		Find(&rows).Error
	if err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, r := range rows {
		m[r.OrderID] = r.ID
	}
	return m, nil
}
