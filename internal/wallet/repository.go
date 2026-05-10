package wallet

import (
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Repository struct{ db *gorm.DB }

func NewRepository(db *gorm.DB) *Repository { return &Repository{db: db} }

func (r *Repository) ByUserID(userID uint) (*Entity, error) {
	var e Entity
	err := r.db.Where("user_id = ?", userID).First(&e).Error
	return &e, err
}

func (r *Repository) CreateIfNotExists(e *Entity) error {
	return r.db.Clauses(clause.OnConflict{DoNothing: true}).Create(e).Error
}

func (r *Repository) AddTransactions(txs []Transaction) error {
	return r.db.Create(&txs).Error
}

func (r *Repository) Transactions(walletID uint) ([]Transaction, error) {
	var list []Transaction
	err := r.db.Where("wallet_id = ?", walletID).Order("created_at DESC").Limit(50).Find(&list).Error
	return list, err
}

func (r *Repository) Transaction(fn func(tx *gorm.DB) error) error {
	return r.db.Transaction(fn)
}
