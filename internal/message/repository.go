package message

import "gorm.io/gorm"

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List() ([]Entity, error) {
	var list []Entity
	err := r.db.Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) Create(e *Entity) error { return r.db.Create(e).Error }
