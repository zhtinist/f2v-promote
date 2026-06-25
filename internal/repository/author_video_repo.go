package repository

import (
	"time"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"github.com/AiMarketool/f2v-promote/pkg/utils"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type AuthorVideoRepo struct {
	db *gorm.DB
}

func NewAuthorVideoRepo(db *gorm.DB) *AuthorVideoRepo {
	return &AuthorVideoRepo{db: db}
}

// GetByNonce 通过 nonce 反查作者视频
func (r *AuthorVideoRepo) GetByNonce(nonce string) (*model.AuthorVideo, error) {
	var v model.AuthorVideo
	if err := r.db.Where("nonce = ?", nonce).First(&v).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// GetByNonceOrDescription 通过 nonce 或 description 反查作者视频（OR 查询）
func (r *AuthorVideoRepo) GetByNonceOrDescription(nonce, description string) (*model.AuthorVideo, error) {
	var v model.AuthorVideo
	q := r.db
	if nonce != "" && description != "" {
		q = q.Where("nonce = ? OR description = ?", nonce, description)
	} else if nonce != "" {
		q = q.Where("nonce = ?", nonce)
	} else if description != "" {
		q = q.Where("description = ?", description)
	} else {
		return nil, nil
	}
	if err := q.First(&v).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// GetByNoncesOrDescriptions 批量通过 nonce 或 description 反查作者视频（IN 查询）
func (r *AuthorVideoRepo) GetByNoncesOrDescriptions(nonces, descriptions []string) ([]model.AuthorVideo, error) {
	validNonces := utils.FilterEmpty(nonces)
	validDescs := utils.FilterEmpty(descriptions)
	if len(validNonces) == 0 && len(validDescs) == 0 {
		return nil, nil
	}

	var list []model.AuthorVideo
	q := r.db
	if len(validNonces) > 0 && len(validDescs) > 0 {
		q = q.Where("nonce IN ? OR description IN ?", validNonces, validDescs)
	} else if len(validNonces) > 0 {
		q = q.Where("nonce IN ?", validNonces)
	} else {
		q = q.Where("description IN ?", validDescs)
	}
	err := q.Find(&list).Error
	return list, err
}

// GetByExportID 通过视频ID反查作者（保留兼容）
func (r *AuthorVideoRepo) GetByExportID(exportID string) (*model.AuthorVideo, error) {
	var v model.AuthorVideo
	if err := r.db.Where("export_id = ?", exportID).First(&v).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// GetAuthorIDByExportID 通过 export_id 查找 author_id
func (r *AuthorVideoRepo) GetAuthorIDByExportID(exportID string) int64 {
	var authorID int64
	r.db.Model(&model.AuthorVideo{}).Where("export_id = ?", exportID).
		Limit(1).Pluck("author_id", &authorID)
	return authorID
}

// ListByAuthorID 获取作者的所有视频
func (r *AuthorVideoRepo) ListByAuthorID(authorID int64) ([]model.AuthorVideo, error) {
	var list []model.AuthorVideo
	err := r.db.Where("author_id = ?", authorID).Order("publish_time DESC").Find(&list).Error
	return list, err
}

// GetExportIDsByAuthorID 获取作者所有 export_id 列表
func (r *AuthorVideoRepo) GetExportIDsByAuthorID(authorID int64) ([]string, error) {
	var ids []string
	err := r.db.Model(&model.AuthorVideo{}).Debug().Where("author_id = ?", authorID).Pluck("export_id", &ids).Error
	return ids, err
}

// BulkUpsert 批量按 nonce 插入或更新
func (r *AuthorVideoRepo) BulkUpsert(videos []model.AuthorVideo) (int, error) {
	count := 0
	for i := range videos {
		err := r.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "nonce"}},
			DoUpdates: clause.AssignmentColumns([]string{"export_id", "description", "cover_url", "publish_time", "raw_data", "updated_at"}),
		}).Create(&videos[i]).Error
		if err != nil {
			return count, err
		}
		count++
	}
	return count, nil
}

// TouchLastChecked 更新视频的水位线为指定时间
func (r *AuthorVideoRepo) TouchLastChecked(id int64, checkedAt time.Time) error {
	return r.db.Model(&model.AuthorVideo{}).
		Where("id = ?", id).
		Update("last_checked_at", checkedAt).Error
}

// VideoInfo 视频简要信息
type VideoInfo struct {
	ExportID    string `json:"export_id"`
	Description string `json:"description"`
}

// GetVideoInfoMap 批量按 ID 查询视频简要信息
func (r *AuthorVideoRepo) GetVideoInfoMap(ids []int64) (map[int64]VideoInfo, error) {
	if len(ids) == 0 {
		return map[int64]VideoInfo{}, nil
	}
	var rows []struct {
		ID          int64  `gorm:"column:id"`
		ExportID    string `gorm:"column:export_id"`
		Description string `gorm:"column:description"`
	}
	if err := r.db.Model(&model.AuthorVideo{}).
		Where("id IN ?", ids).
		Select("id, export_id, description").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]VideoInfo, len(rows))
	for _, r := range rows {
		m[r.ID] = VideoInfo{ExportID: r.ExportID, Description: r.Description}
	}
	return m, nil
}

// SearchIDsByDescription 按描述模糊搜索视频 ID 列表
func (r *AuthorVideoRepo) SearchIDsByDescription(keyword string) ([]int64, error) {
	if keyword == "" {
		return nil, nil
	}
	var ids []int64
	err := r.db.Model(&model.AuthorVideo{}).
		Where("description LIKE ?", "%"+keyword+"%").
		Pluck("id", &ids).Error
	return ids, err
}

// ListPaged 分页查询视频列表
func (r *AuthorVideoRepo) ListPaged(authorID int64, platform string, page, pageSize int) ([]model.AuthorVideo, int64, error) {
	var list []model.AuthorVideo
	var total int64

	q := r.db.Model(&model.AuthorVideo{})
	if authorID > 0 {
		q = q.Where("author_id = ?", authorID)
	}
	if platform != "" {
		// 通过 author 的 platform 字段筛选（需 JOIN）
		q = q.Joins("JOIN authors ON authors.id = author_videos.author_id AND authors.platform = ?", platform)
	}

	q.Count(&total)

	offset := (page - 1) * pageSize
	err := q.Select("author_videos.*").
		Order("author_videos.created_at DESC").
		Offset(offset).Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

// GetByID 按主键查询
func (r *AuthorVideoRepo) GetByID(id int64) (*model.AuthorVideo, error) {
	var v model.AuthorVideo
	if err := r.db.Where("id = ?", id).First(&v).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &v, nil
}

// Count 视频总数
func (r *AuthorVideoRepo) Count() (int64, error) {
	var count int64
	err := r.db.Model(&model.AuthorVideo{}).Count(&count).Error
	return count, err
}

// GetByIDs 批量通过主键 ID 查询作者视频
func (r *AuthorVideoRepo) GetByIDs(ids []int64) ([]model.AuthorVideo, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []model.AuthorVideo
	err := r.db.Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// GetByExportIDs 批量通过 export_id 查询作者视频（用于飞书同步获取 create_time）
func (r *AuthorVideoRepo) GetByExportIDs(exportIDs []string) ([]model.AuthorVideo, error) {
	if len(exportIDs) == 0 {
		return nil, nil
	}
	var list []model.AuthorVideo
	err := r.db.Where("export_id IN ?", exportIDs).Find(&list).Error
	return list, err
}
