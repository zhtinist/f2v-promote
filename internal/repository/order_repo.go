package repository

import (
	"encoding/json"
	"time"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

type OrderRepo struct {
	db *gorm.DB
}

func NewOrderRepo(db *gorm.DB) *OrderRepo {
	return &OrderRepo{db: db}
}

// CreateZhuge 创建订单，初始状态 init（还没调诸葛）
func (r *OrderRepo) CreateZhuge(campaignID, userID, accountID int64, tagGroup datatypes.JSON, createResp datatypes.JSON) (*model.Order, error) {
	o := model.Order{
		CampaignID:     campaignID,
		UserID:         userID,
		AccountID:      accountID,
		TagGroup:       tagGroup,
		Status:         model.OrderStatusInit,
		Source:         model.PlatformWeixin,
		CreateResponse: createResp,
	}
	if err := r.db.Create(&o).Error; err != nil {
		return nil, err
	}
	return &o, nil
}

func (r *OrderRepo) GetByID(id int64) (*model.Order, error) {
	var o model.Order
	if err := r.db.Where("id = ?", id).First(&o).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &o, nil
}

func (r *OrderRepo) GetByCampaignID(campaignID int64) ([]model.Order, error) {
	var list []model.Order
	err := r.db.Where("campaign_id = ?", campaignID).Order("created_at DESC").Find(&list).Error
	return list, err
}

// UpdateStatus 更新订单状态
func (r *OrderRepo) UpdateStatus(id int64, status string, closeReason *string) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		updates := map[string]any{
			"status": status,
		}
		if closeReason != nil {
			updates["close_reason"] = *closeReason
		}
		if err := tx.Model(&model.Order{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}

		if !model.IsTerminalStatus(status) {
			return nil
		}

		var order model.Order
		if err := tx.Select("campaign_id").Where("id = ?", id).First(&order).Error; err != nil {
			return err
		}

		var nonTerminal int64
		if err := tx.Model(&model.Order{}).
			Where("campaign_id = ? AND status NOT IN ?", order.CampaignID,
				[]string{model.OrderStatusFailed, model.OrderStatusClosed}).
			Count(&nonTerminal).Error; err != nil {
			return err
		}

		if nonTerminal == 0 {
			now := time.Now()
			return tx.Model(&model.Campaign{}).Where("id = ?", order.CampaignID).
				Updates(map[string]any{
					"status":      "cancelled",
					"finished_at": &now,
				}).Error
		}

		return nil
	})
}

// UpdateZhugeSubmitted 诸葛创建成功后更新：init → pending
func (r *OrderRepo) UpdateZhugeSubmitted(id int64, batchID, payURL string) error {
	return r.db.Model(&model.Order{}).Where("id = ?", id).
		Updates(map[string]any{
			"batch_id": batchID,
			"pay_url":  payURL,
			"status":   model.OrderStatusPending,
		}).Error
}

func (r *OrderRepo) AppendQueryResponse(id int64, item any) error {
	const maxQueryLogs = 15

	var order model.Order
	if err := r.db.Select("query_response").Where("id = ?", id).First(&order).Error; err != nil {
		return err
	}

	var arr []any
	if len(order.QueryResponse) > 0 {
		if err := json.Unmarshal(order.QueryResponse, &arr); err != nil {
			arr = nil
		}
	}
	arr = append(arr, item)

	// 仅保留最新 maxQueryLogs 条
	if len(arr) > maxQueryLogs {
		arr = arr[len(arr)-maxQueryLogs:]
	}

	data, err := json.Marshal(arr)
	if err != nil {
		return err
	}
	return r.db.Model(&model.Order{}).Where("id = ?", id).
		Update("query_response", datatypes.JSON(data)).Error
}

func (r *OrderRepo) UpdatePlatformID(id int64, platformOrderID string) error {
	return r.db.Model(&model.Order{}).Where("id = ?", id).
		Update("platform_order_id", platformOrderID).Error
}

func (r *OrderRepo) GetNeedPolling(batchSize int, minInterval time.Duration) ([]model.Order, error) {
	var list []model.Order
	cutoff := time.Now().Add(-minInterval)
	err := r.db.Debug().
		Where("(platform_order_id IS NOT NULL AND platform_order_id != '') OR (batch_id IS NOT NULL AND batch_id != '')").
		Where("status NOT IN ?", []string{model.OrderStatusFailed, model.OrderStatusClosed, model.OrderStatusInit}).
		Where("query_at IS NULL OR query_at < ?", cutoff).
		Order("query_at ASC, created_at ASC").
		Limit(batchSize).
		Find(&list).Error
	return list, err
}

func (r *OrderRepo) TouchQueryAt(id int64) error {
	now := time.Now()
	return r.db.Model(&model.Order{}).Where("id = ?", id).Update("query_at", now).Error
}

func (r *OrderRepo) GetRecentByUserID(userID int64, limit int) ([]model.Order, error) {
	var list []model.Order
	err := r.db.Omit("create_response", "query_response", "latest_detail", "latest_record").
		Where("user_id = ?", userID).
		Order("created_at DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}

func (r *OrderRepo) PaginateByUserID(userID int64, status string, filterID int64, startDate, endDate string, page, pageSize int) ([]model.Order, int64, error) {
	q := r.db.Model(&model.Order{}).Where("user_id = ?", userID)
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
	var list []model.Order
	err := q.Omit("create_response", "query_response", "latest_record").
		Order("created_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *OrderRepo) GetStatsByUserID(userID int64) (map[string]any, error) {
	type statusCount struct {
		Status string
		Count  int64
	}
	var counts []statusCount
	err := r.db.Model(&model.Order{}).
		Select("status, count(*) as count").
		Where("user_id = ?", userID).
		Group("status").
		Scan(&counts).Error
	if err != nil {
		return nil, err
	}

	stats := map[string]any{
		"active":  int64(0),
		"pending": int64(0),
		"review":  int64(0),
		"failed":  int64(0),
		"closed":  int64(0),
		"total":   int64(0),
	}
	var total int64
	for _, sc := range counts {
		stats[sc.Status] = sc.Count
		total += sc.Count
	}
	stats["total"] = total

	var totalCost float64
	r.db.Model(&model.Order{}).
		Select("COALESCE(SUM(CAST(JSON_UNQUOTE(JSON_EXTRACT(latest_detail, '$.cost')) AS DECIMAL(10,2))), 0)").
		Where("user_id = ? AND latest_detail IS NOT NULL", userID).
		Scan(&totalCost)
	stats["total_cost"] = totalCost

	return stats, nil
}

func (r *OrderRepo) UpdateLatestDetail(id int64, detail datatypes.JSON) error {
	return r.db.Model(&model.Order{}).Where("id = ?", id).
		Update("latest_detail", detail).Error
}

func (r *OrderRepo) UpdateLatestRecord(id int64, record datatypes.JSON) error {
	return r.db.Model(&model.Order{}).Where("id = ?", id).
		Update("latest_record", record).Error
}

func (r *OrderRepo) UpdateCreateRequest(id int64, data datatypes.JSON) error {
	return r.db.Model(&model.Order{}).Where("id = ?", id).
		Update("create_request", data).Error
}
