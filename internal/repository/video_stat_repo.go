package repository

import (
	"time"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type VideoStatRepo struct {
	db *gorm.DB
}

func NewVideoStatRepo(db *gorm.DB) *VideoStatRepo {
	return &VideoStatRepo{db: db}
}

// Upsert 插入单条 video_stat
func (r *VideoStatRepo) Upsert(stat *model.VideoStat) error {
	return r.db.Create(stat).Error
}

// BulkUpsert 批量插入或更新
func (r *VideoStatRepo) BulkUpsert(stats []model.VideoStat) (int, error) {
	count := 0
	for i := range stats {
		if err := r.Upsert(&stats[i]); err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// GetByID 按主键查询
func (r *VideoStatRepo) GetByID(id int64) (*model.VideoStat, error) {
	var stat model.VideoStat
	if err := r.db.Where("id = ?", id).First(&stat).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &stat, nil
}

// GetByExportID 按视频 ID 查询
func (r *VideoStatRepo) GetByExportID(exportID string) (*model.VideoStat, error) {
	var stat model.VideoStat
	if err := r.db.Where("export_id = ?", exportID).First(&stat).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &stat, nil
}

// List 分页查询，按发布时间倒序
func (r *VideoStatRepo) List(page, pageSize int) ([]model.VideoStat, int64, error) {
	var list []model.VideoStat
	var total int64

	r.db.Model(&model.VideoStat{}).Count(&total)

	offset := (page - 1) * pageSize
	err := r.db.Order("publish_date DESC, created_at DESC").
		Offset(offset).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// Delete 删除
func (r *VideoStatRepo) Delete(id int64) error {
	return r.db.Where("id = ?", id).Delete(&model.VideoStat{}).Error
}

// GetUnsyncedByAuthorID 获取某作者未同步到飞书的 video_stats
func (r *VideoStatRepo) GetUnsyncedByAuthorID(authorID int64, lastSyncedAt time.Time) ([]model.VideoStat, error) {
	var list []model.VideoStat
	err := r.db.Where("author_id = ? AND feishu_synced = 0 AND created_at > ?", authorID, lastSyncedAt.Format(time.DateTime)).
		Order("created_at ASC").
		Find(&list).Error
	return list, err
}

// MarkSynced 批量标记为已同步
func (r *VideoStatRepo) MarkSynced(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Model(&model.VideoStat{}).Where("id IN ?", ids).Update("feishu_synced", true).Error
}

// UpdateAuthorID 回填单条 video_stats 的 author_id
func (r *VideoStatRepo) UpdateAuthorID(statID int64, authorID int64) error {
	return r.db.Model(&model.VideoStat{}).Where("id = ?", statID).Update("author_id", authorID).Error
}

// BatchUpdateAuthorID 批量回填 author_id（key=statID, value=authorID）
func (r *VideoStatRepo) BatchUpdateAuthorID(updates map[int64]int64) error {
	for statID, authorID := range updates {
		if err := r.UpdateAuthorID(statID, authorID); err != nil {
			return err
		}
	}
	return nil
}

// BatchUpdateAuthorAndVideoID 批量回填 author_id + author_video_id
func (r *VideoStatRepo) BatchUpdateAuthorAndVideoID(authorIDs, avIDs map[int64]int64) error {
	for statID, authorID := range authorIDs {
		fields := map[string]interface{}{
			"author_id": authorID,
		}
		if avID, ok := avIDs[statID]; ok && avID != 0 {
			fields["author_video_id"] = avID
		}
		if err := r.db.Model(&model.VideoStat{}).Where("id = ?", statID).Updates(fields).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetUnmatchedStats 获取 author_id 或 author_video_id 为空的 video_stats（供定时任务回填）
func (r *VideoStatRepo) GetUnmatchedStats() ([]model.VideoStat, error) {
	var list []model.VideoStat
	err := r.db.Debug().Where("author_id IS NULL OR IFNULL(author_video_id, 0) = 0").
		Order("created_at desc").
		Find(&list).Error
	return list, err
}

// GetLatestTwoByExportID 获取某视频最近两条记录（用于计算播放增量）
func (r *VideoStatRepo) GetLatestTwoByExportID(exportID string) ([]model.VideoStat, error) {
	var list []model.VideoStat
	err := r.db.Where("export_id = ?", exportID).
		Order("created_at DESC").
		Limit(2).
		Find(&list).Error
	return list, err
}

// GetLatestThreeByExportID 获取某视频最近三条记录（用于环比衰减计算）
// 返回 [newest, middle, oldest]（按 created_at DESC）
func (r *VideoStatRepo) GetLatestThreeByExportID(exportID string) ([]model.VideoStat, error) {
	var list []model.VideoStat
	err := r.db.Where("export_id = ?", exportID).
		Order("created_at DESC").
		Limit(3).
		Find(&list).Error
	return list, err
}

// GetNextTwoByAuthorVideo 倒序查询最新两条记录用于增量对比
// afterTime 有值时仅查 collect_date > afterTime 的记录（水位线过滤）
// 返回 [older, newer] 顺序
func (r *VideoStatRepo) GetNextTwoByAuthorVideo(authorVideoID int64, description string, afterTime *time.Time) ([]model.VideoStat, error) {
	videoWhere := r.db.Where("author_video_id = ?", authorVideoID)
	if description != "" {
		videoWhere = videoWhere.Or("TRIM(description) = TRIM(?) AND TRIM(description) != ''", description)
	}

	q := r.db.Where(videoWhere)
	if afterTime != nil {
		q = q.Where("collect_date > ?", afterTime.Format("2006-01-02 15:04:05"))
	}

	var records []model.VideoStat
	if err := q.Order("created_at DESC").Limit(2).Find(&records).Error; err != nil {
		return nil, err
	}
	if len(records) < 2 {
		return nil, nil
	}

	// 倒序 [newer, older] → 翻转为 [older, newer]
	records[0], records[1] = records[1], records[0]
	return records, nil
}

// MarkPromoteScanned 批量标记 video_stats 为已扫描
func (r *VideoStatRepo) MarkPromoteScanned(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return r.db.Model(&model.VideoStat{}).
		Where("id IN ?", ids).
		Update("promote_scanned", true).Error
}

// GetLatestByExportIDs 批量获取多个视频的最新记录
// 通过子查询取每个 export_id 的最大 id，再批量查出完整记录
func (r *VideoStatRepo) GetLatestByExportIDs(exportIDs []string) ([]model.VideoStat, error) {
	if len(exportIDs) == 0 {
		return nil, nil
	}
	var list []model.VideoStat
	subQuery := r.db.Model(&model.VideoStat{}).
		Select("MAX(id) as id").
		Where("export_id IN ?", exportIDs).
		Group("export_id")
	err := r.db.Where("id IN (?)", subQuery).Find(&list).Error
	return list, err
}

// ListByAuthorVideoID 通过 author_video_id 查找关联统计
func (r *VideoStatRepo) ListByAuthorVideoID(authorVideoID int64) ([]model.VideoStat, error) {
	var list []model.VideoStat
	err := r.db.Where("author_video_id = ?", authorVideoID).
		Select("id, export_id, description, author_id, author_video_id, collect_date, play_count, like_count, comment_count, share_count, follow_count, completion_rate, feishu_synced, created_at").
		Order("collect_date desc").
		Find(&list).Error
	return list, err
}

// ListFiltered 分页筛选查询（排除大字段）
func (r *VideoStatRepo) ListFiltered(authorID, authorVideoID int64, dateFrom, dateTo string, feishuSynced *bool, page, pageSize int) ([]model.VideoStat, int64, error) {
	var list []model.VideoStat
	var total int64

	q := r.db.Model(&model.VideoStat{})
	if authorID > 0 {
		q = q.Where("author_id = ?", authorID)
	}
	if authorVideoID > 0 {
		q = q.Where("author_video_id = ?", authorVideoID)
	}
	if dateFrom != "" {
		q = q.Where("collect_date >= ?", dateFrom)
	}
	if dateTo != "" {
		q = q.Where("collect_date <= ?", dateTo)
	}
	if feishuSynced != nil {
		q = q.Where("feishu_synced = ?", *feishuSynced)
	}

	q.Count(&total)

	offset := (page - 1) * pageSize
	err := q.Select("id, export_id, description, author_id, author_video_id, collect_date, play_count, like_count, comment_count, share_count, follow_count, completion_rate, feishu_synced, created_at").
		Order("collect_date DESC, created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

// TodayCount 今日采集数
func (r *VideoStatRepo) TodayCount() (int64, error) {
	var count int64
	err := r.db.Model(&model.VideoStat{}).
		Where("DATE(collect_date) = CURDATE()").
		Count(&count).Error
	return count, err
}

// GetPreviousStat 获取同 export_id 在 currentID 之前的最近一条记录（用于增量计算）
func (r *VideoStatRepo) GetPreviousStat(exportID string, currentID int64) (*model.VideoStat, error) {
	var stat model.VideoStat
	err := r.db.Where("export_id = ? AND id < ?", exportID, currentID).
		Order("id DESC").
		Limit(1).
		First(&stat).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &stat, nil
}
