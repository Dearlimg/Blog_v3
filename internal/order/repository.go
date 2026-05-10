package order

import (
	"blog-front/internal/product"

	"gorm.io/gorm"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

// --- Order ---

func (r *Repository) CreateOrder(o *Entity, tx ...*gorm.DB) error {
	if len(tx) > 0 {
		return tx[0].Create(o).Error
	}
	return r.db.Create(o).Error
}

func (r *Repository) ListOrders(userID uint, page, pageSize int) ([]Entity, int64, error) {
	var total int64
	r.db.Model(&Entity{}).Where("user_id = ?", userID).Count(&total)

	var list []Entity
	err := r.db.Where("user_id = ?", userID).
		Preload("Product").
		Order("created_at DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

func (r *Repository) OrderByID(userID, id uint) (*Entity, error) {
	var e Entity
	err := r.db.Preload("Product").Where("id = ? AND user_id = ?", id, userID).First(&e).Error
	return &e, err
}

// --- Product (read-only cross-domain) ---

func (r *Repository) ProductByID(id uint) (*product.Entity, error) {
	var p product.Entity
	err := r.db.First(&p, id).Error
	return &p, err
}

func (r *Repository) DeductStock(p *product.Entity, qty int, tx *gorm.DB) error {
	return tx.Model(p).Update("stock", gorm.Expr("stock - ?", qty)).Error
}

// --- Cart ---

func (r *Repository) CartItems(userID uint) ([]CartItem, error) {
	var list []CartItem
	err := r.db.Where("user_id = ?", userID).Preload("Product").Order("created_at DESC").Find(&list).Error
	return list, err
}

func (r *Repository) CartItemByProduct(userID, productID uint) (*CartItem, error) {
	var item CartItem
	err := r.db.Where("user_id = ? AND product_id = ?", userID, productID).First(&item).Error
	return &item, err
}

func (r *Repository) CreateCartItem(item *CartItem) error { return r.db.Create(item).Error }

func (r *Repository) SaveCartItem(item *CartItem) error { return r.db.Save(item).Error }

func (r *Repository) CartItemByID(id uint) (*CartItem, error) {
	var item CartItem
	err := r.db.First(&item, id).Error
	return &item, err
}

func (r *Repository) DeleteCartItem(item *CartItem) error { return r.db.Delete(item).Error }

func (r *Repository) ClearCart(userID uint, tx *gorm.DB) error {
	return tx.Where("user_id = ?", userID).Delete(&CartItem{}).Error
}

func (r *Repository) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}
