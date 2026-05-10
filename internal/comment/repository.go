package comment

import "gorm.io/gorm"

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ListRoot(page, pageSize int) ([]Entity, int64, error) {
	var total int64
	r.db.Model(&Entity{}).Where("parent_id IS NULL").Count(&total)

	var list []Entity
	err := r.db.Where("parent_id IS NULL").
		Preload("Replies", func(db *gorm.DB) *gorm.DB {
			return db.Order("created_at ASC")
		}).
		Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error

	return list, total, err
}

func (r *Repository) ByID(id uint) (*Entity, error) {
	var e Entity
	err := r.db.First(&e, id).Error
	return &e, err
}

func (r *Repository) Create(e *Entity) error { return r.db.Create(e).Error }

func (r *Repository) Save(e *Entity) error { return r.db.Save(e).Error }

func (r *Repository) Delete(e *Entity) error { return r.db.Delete(e).Error }

func (r *Repository) FillUsername(e *Entity) {
	var name string
	r.db.Table("users").Select("username").Where("id = ?", e.UserID).Scan(&name)
	e.Username = name
}

func (r *Repository) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}
