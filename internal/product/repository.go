package product

import "gorm.io/gorm"

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) List(page, pageSize int, category, keyword string) ([]Entity, int64, error) {
	q := r.db.Model(&Entity{})
	if category != "" {
		q = q.Where("category = ?", category)
	}
	if keyword != "" {
		q = q.Where("name LIKE ?", "%"+keyword+"%")
	}

	var total int64
	q.Count(&total)

	var list []Entity
	err := q.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&list).Error
	return list, total, err
}

func (r *Repository) ByID(id uint) (*Entity, error) {
	var e Entity
	err := r.db.First(&e, id).Error
	return &e, err
}

func (r *Repository) Create(e *Entity) error { return r.db.Create(e).Error }

func (r *Repository) Update(e *Entity, updates map[string]any) error {
	return r.db.Model(e).Updates(updates).Error
}

func (r *Repository) Delete(e *Entity) error { return r.db.Delete(e).Error }
