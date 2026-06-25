package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PromptRepo struct {
	db *gorm.DB
}

func NewPromptRepo(db *gorm.DB) *PromptRepo {
	return &PromptRepo{db: db}
}

// Get returns a prompt template by name, or nil if not found.
func (r *PromptRepo) Get(name string) (*model.PromptTemplate, error) {
	var p model.PromptTemplate
	if err := r.db.Where("name = ?", name).First(&p).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &p, nil
}

// GetAll returns all prompt templates ordered by created_at desc.
func (r *PromptRepo) GetAll() ([]model.PromptTemplate, error) {
	var list []model.PromptTemplate
	err := r.db.Order("created_at DESC").Find(&list).Error
	return list, err
}

// Upsert creates or updates a prompt template by name.
func (r *PromptRepo) Upsert(name, title, content string, description *string) (*model.PromptTemplate, error) {
	p := model.PromptTemplate{
		Name:        name,
		Title:       title,
		Content:     content,
		Description: description,
	}
	err := r.db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "name"}},
		DoUpdates: clause.AssignmentColumns([]string{"title", "content", "description", "updated_at"}),
	}).Create(&p).Error
	if err != nil {
		return nil, err
	}
	return r.Get(name)
}

// Delete removes a prompt template by name.
func (r *PromptRepo) Delete(name string) (bool, error) {
	result := r.db.Where("name = ?", name).Delete(&model.PromptTemplate{})
	if result.Error != nil {
		return false, result.Error
	}
	return result.RowsAffected > 0, nil
}
