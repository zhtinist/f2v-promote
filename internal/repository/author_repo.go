package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type AuthorRepo struct {
	db *gorm.DB
}

func NewAuthorRepo(db *gorm.DB) *AuthorRepo {
	return &AuthorRepo{db: db}
}

func (r *AuthorRepo) ListByAccountID(accountID int64) ([]model.Author, error) {
	var list []model.Author
	err := r.db.Where("account_id = ?", accountID).Order("cached_at DESC").Find(&list).Error
	return list, err
}

func (r *AuthorRepo) ListByAccountIDPaged(accountID int64, page, pageSize int) ([]model.Author, int64, error) {
	var total int64
	q := r.db.Model(&model.Author{}).Where("account_id = ?", accountID)
	q.Count(&total)
	var list []model.Author
	err := q.Order("cached_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *AuthorRepo) ListAllPaged(page, pageSize int) ([]model.Author, int64, error) {
	var total int64
	r.db.Model(&model.Author{}).Count(&total)
	var list []model.Author
	err := r.db.Order("cached_at DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

// UpsertByAccountID 按 username 匹配：存在则更新 API 字段，不存在则新增。保留 feishu 相关字段不被覆盖。
func (r *AuthorRepo) ReplaceByAccountID(accountID int64, authors []model.Author) error {
	if len(authors) == 0 {
		return nil
	}
	return r.db.Transaction(func(tx *gorm.DB) error {
		for i := range authors {
			a := &authors[i]
			var existing model.Author
			err := tx.Where("username = ? AND account_id = ?", a.Username, accountID).First(&existing).Error
			if err == gorm.ErrRecordNotFound {
				// 新作者，直接插入
				if err := tx.Create(a).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else {
				// 已存在，只更新 API 来源字段，保留 feishu 字段
				if err := tx.Model(&existing).Updates(map[string]interface{}{
					"nickname":   a.Nickname,
					"avatar_url": a.AvatarURL,
					"platform":   a.Platform,
					"raw_data":   a.RawData,
					"cached_at":  a.CachedAt,
				}).Error; err != nil {
					return err
				}
			}
		}
		return nil
	})
}

// ListFeishuEnabled 获取所有启用飞书同步的作者
func (r *AuthorRepo) ListFeishuEnabled() ([]model.Author, error) {
	var list []model.Author
	err := r.db.Where("feishu_sync_enabled = ?", true).Find(&list).Error
	return list, err
}

// GetByID 按主键查询单个作者
func (r *AuthorRepo) GetByID(id int64) (*model.Author, error) {
	var author model.Author
	if err := r.db.Where("id = ?", id).First(&author).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &author, nil
}

// ListAll 获取所有作者
func (r *AuthorRepo) ListAll() ([]model.Author, error) {
	var list []model.Author
	err := r.db.Find(&list).Error
	return list, err
}

// GetNicknameMap 批量获取作者昵称 map[authorID]nickname
func (r *AuthorRepo) GetNicknameMap(ids []int64) (map[int64]string, error) {
	if len(ids) == 0 {
		return map[int64]string{}, nil
	}
	var authors []model.Author
	if err := r.db.Where("id IN ?", ids).Select("id, nickname").Find(&authors).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]string, len(authors))
	for _, a := range authors {
		m[a.ID] = a.Nickname
	}
	return m, nil
}

// Count 作者总数
func (r *AuthorRepo) Count() (int64, error) {
	var count int64
	err := r.db.Model(&model.Author{}).Count(&count).Error
	return count, err
}
