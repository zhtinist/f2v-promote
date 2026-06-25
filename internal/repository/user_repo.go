package repository

import (
	"github.com/AiMarketool/f2v-promote/internal/model"
	"gorm.io/gorm"
)

type UserRepo struct {
	db *gorm.DB
}

func NewUserRepo(db *gorm.DB) *UserRepo {
	return &UserRepo{db: db}
}

// GetByUsername returns a user by username, or nil if not found.
func (r *UserRepo) GetByUsername(username string) (*model.User, error) {
	var u model.User
	if err := r.db.Where("username = ?", username).First(&u).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// GetByID returns a user by primary key.
func (r *UserRepo) GetByID(id int64) (*model.User, error) {
	var u model.User
	if err := r.db.Where("id = ?", id).First(&u).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// Count returns the total number of users.
func (r *UserRepo) Count() (int64, error) {
	var count int64
	err := r.db.Model(&model.User{}).Count(&count).Error
	return count, err
}

// Create inserts a new user.
func (r *UserRepo) Create(username, hashedPassword string) (*model.User, error) {
	u := model.User{
		Username:       username,
		HashedPassword: hashedPassword,
	}
	if err := r.db.Create(&u).Error; err != nil {
		return nil, err
	}
	return &u, nil
}

// GetThreshold returns the cost_threshold for a user.
func (r *UserRepo) GetThreshold(userID int64) (float64, error) {
	var u model.User
	if err := r.db.Select("cost_threshold").Where("id = ?", userID).First(&u).Error; err != nil {
		return 0, err
	}
	return u.CostThreshold, nil
}

// UpdateThreshold sets a new cost_threshold for a user.
func (r *UserRepo) UpdateThreshold(userID int64, threshold float64) error {
	return r.db.Model(&model.User{}).Where("id = ?", userID).Update("cost_threshold", threshold).Error
}
