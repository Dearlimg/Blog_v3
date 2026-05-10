package message

import (
	"time"

	"gorm.io/gorm"
)

type Entity struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Name      string         `json:"name" gorm:"size:100;not null"`
	Email     string         `json:"email" gorm:"size:200"`
	Content   string         `json:"content" gorm:"type:text;not null"`
	CreatedAt time.Time      `json:"create_at"`
	UpdatedAt time.Time      `json:"update_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

type CreateReq struct {
	Name    string `json:"name" binding:"required"`
	Email   string `json:"email"`
	Content string `json:"content" binding:"required"`
}
