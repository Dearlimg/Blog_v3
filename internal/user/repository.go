package user

import "gorm.io/gorm"

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ByEmail(email string) (*Entity, error) {
	var e Entity
	err := r.db.Where("email = ?", email).First(&e).Error
	return &e, err
}

func (r *Repository) ByID(id uint) (*Entity, error) {
	var e Entity
	err := r.db.First(&e, id).Error
	return &e, err
}

func (r *Repository) Create(e *Entity) error { return r.db.Create(e).Error }

func (r *Repository) Save(e *Entity) error { return r.db.Save(e).Error }
