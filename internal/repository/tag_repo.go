package repository

import (
	"strconv"

	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type ZhugeTagRepo struct {
	db *gorm.DB
}

func NewZhugeTagRepo(db *gorm.DB) *ZhugeTagRepo {
	return &ZhugeTagRepo{db: db}
}

// GetAll returns every tag row.
func (r *ZhugeTagRepo) GetAll() ([]model.ZhugeTag, error) {
	var tags []model.ZhugeTag
	err := r.db.Find(&tags).Error
	return tags, err
}

// SaveBulk upserts tags in batches of 500 to avoid MySQL placeholder limit.
func (r *ZhugeTagRepo) SaveBulk(tags []model.ZhugeTag) error {
	const batchSize = 500
	for i := 0; i < len(tags); i += batchSize {
		end := i + batchSize
		if end > len(tags) {
			end = len(tags)
		}
		batch := tags[i:end]
		if err := r.db.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "id"}},
			DoUpdates: clause.AssignmentColumns([]string{"text", "parent_id", "parent_text", "wx_level", "updated_at"}),
		}).Create(&batch).Error; err != nil {
			return err
		}
	}
	return nil
}

// GetFlat returns a map of tag text -> TagMeta{ID, WxLevel} for all tags.
func (r *ZhugeTagRepo) GetFlat() (map[string]model.TagMeta, error) {
	var tags []model.ZhugeTag
	if err := r.db.Select("id", "text", "wx_level").Find(&tags).Error; err != nil {
		return nil, err
	}
	m := make(map[string]model.TagMeta, len(tags))
	for _, t := range tags {
		m[t.Text] = model.TagMeta{ID: t.ID, WxLevel: t.WxLevel}
	}
	return m, nil
}

// ListPaged 分页查询标签
func (r *ZhugeTagRepo) ListPaged(keyword, level, parentID string, page, pageSize int) ([]model.ZhugeTag, int64, error) {
	var list []model.ZhugeTag
	var total int64

	q := r.db.Model(&model.ZhugeTag{})

	if keyword != "" {
		q = q.Where("text LIKE ?", "%"+keyword+"%")
	}
	if level != "" {
		if lv, err := strconv.Atoi(level); err == nil {
			q = q.Where("wx_level = ?", lv)
		}
	}
	if parentID != "" {
		q = q.Where("parent_id = ?", parentID)
	}

	q.Count(&total)

	offset := (page - 1) * pageSize
	err := q.Order("level ASC, text ASC").Offset(offset).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// GetCategories 获取所有一级分类标签 (level=1)
func (r *ZhugeTagRepo) GetCategories() ([]model.ZhugeTag, error) {
	var tags []model.ZhugeTag
	err := r.db.Where("level = 1").Order("text ASC").Find(&tags).Error
	return tags, err
}

// GetChildrenByCategoryIDs 通过关联表获取分类下所有二级标签（多对多去重）
func (r *ZhugeTagRepo) GetChildrenByCategoryIDs(categoryIDs []string) ([]model.ZhugeTag, error) {
	if len(categoryIDs) == 0 {
		return nil, nil
	}
	var tags []model.ZhugeTag
	err := r.db.Distinct().
		Joins("JOIN zhuge_tag_categories tc ON zhuge_tags.id = tc.tag_id").
		Where("tc.category_id IN ?", categoryIDs).
		Find(&tags).Error
	return tags, err
}
