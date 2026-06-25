package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type FeishuSyncCursorRepo struct {
	db *gorm.DB
}

func NewFeishuSyncCursorRepo(db *gorm.DB) *FeishuSyncCursorRepo {
	return &FeishuSyncCursorRepo{db: db}
}

// GetByAuthorID 按作者 ID 查询同步游标
func (r *FeishuSyncCursorRepo) GetByAuthorID(authorID int64) (*model.FeishuSyncCursor, error) {
	var cursor model.FeishuSyncCursor
	if err := r.db.Where("author_id = ?", authorID).First(&cursor).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &cursor, nil
}

// Upsert 更新或新建同步游标
func (r *FeishuSyncCursorRepo) Upsert(cursor *model.FeishuSyncCursor) error {
	return r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "author_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_synced_at", "sync_count", "updated_at"}),
	}).Create(cursor).Error
}
