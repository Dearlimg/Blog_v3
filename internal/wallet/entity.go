package wallet

import (
	"time"

	"gorm.io/gorm"
)

type Entity struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	UserID    uint           `json:"user_id" gorm:"uniqueIndex;not null"`
	Balance   float64        `json:"balance" gorm:"default:0"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Entity) TableName() string { return "wallets" }

type Transaction struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	WalletID    uint           `json:"wallet_id" gorm:"index;not null"`
	Amount      float64        `json:"amount" gorm:"not null"`
	Type        string         `json:"type" gorm:"size:50;not null;index"`
	Description string         `json:"description" gorm:"size:500"`
	RelatedID   *uint          `json:"related_id"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Transaction) TableName() string { return "transactions" }

type AddBalanceReq struct {
	Amount      float64 `json:"amount" binding:"required,gt=0"`
	Description string  `json:"description"`
}

type TransferReq struct {
	ToUserID    uint    `json:"to_user_id" binding:"required"`
	Amount      float64 `json:"amount" binding:"required,gt=0"`
	Description string  `json:"description"`
}
