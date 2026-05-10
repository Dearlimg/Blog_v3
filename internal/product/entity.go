package product

import (
	"time"

	"gorm.io/gorm"
)

type Entity struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	Name        string         `json:"name" gorm:"size:200;not null"`
	Description string         `json:"description" gorm:"type:text"`
	Price       float64        `json:"price" gorm:"not null"`
	Category    string         `json:"category" gorm:"size:100;index"`
	ImageURL    string         `json:"image_url" gorm:"size:500"`
	Stock       int            `json:"stock" gorm:"default:0"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}
