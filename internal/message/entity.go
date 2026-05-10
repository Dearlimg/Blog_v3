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
	IP        string         `json:"ip" gorm:"size:45"`
	CreatedAt time.Time      `json:"create_at"`
	UpdatedAt time.Time      `json:"update_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Entity) TableName() string { return "messages" }

type CreateReq struct {
	Name    string `json:"name"`
	Email   string `json:"email"`
	Content string `json:"content" binding:"required"`
}
