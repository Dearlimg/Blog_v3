package message

import "gorm.io/gorm"

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(page, pageSize int) ([]Entity, int64, error) {
	var total int64
	if err := r.db.Model(&Entity{}).Count(&total).Error; err != nil {
		return nil, 0, err
	}

	list := []Entity{}
	err := r.db.Order("created_at DESC, id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *Repository) Create(e *Entity) error { return r.db.Create(e).Error }
