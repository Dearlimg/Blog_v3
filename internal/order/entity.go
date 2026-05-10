package order

import (
	"time"

	"blog-front/internal/product"

	"gorm.io/gorm"
)

type Entity struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	UserID    uint           `json:"user_id" gorm:"index;not null"`
	ProductID uint           `json:"product_id" gorm:"not null"`
	Quantity  int            `json:"quantity" gorm:"not null"`
	Address   string         `json:"address" gorm:"size:500;not null"`
	Status    string         `json:"status" gorm:"size:50;default:pending;index"`
	Total     float64        `json:"total" gorm:"not null"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	Product product.Entity `json:"product" gorm:"foreignKey:ProductID"`
}

type CreateReq struct {
	ProductID uint   `json:"product_id" binding:"required"`
	Quantity  int    `json:"quantity" binding:"required,gt=0"`
	Address   string `json:"address" binding:"required"`
}

type CartItem struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	UserID    uint           `json:"user_id" gorm:"index;not null"`
	ProductID uint           `json:"product_id" gorm:"not null"`
	Quantity  int            `json:"quantity" gorm:"default:1"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`

	Product product.Entity `json:"product" gorm:"foreignKey:ProductID"`
}

type AddCartReq struct {
	ProductID uint `json:"product_id" binding:"required"`
	Quantity  int  `json:"quantity" binding:"required,gt=0"`
}

type UpdateCartReq struct {
	Quantity int `json:"quantity" binding:"required,gt=0"`
}

type CheckoutReq struct {
	Address string `json:"address" binding:"required"`
}
