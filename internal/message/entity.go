package message

import (
	"time"

	"gorm.io/gorm"
)

type Entity struct {
	ID        uint           `json:"id" gorm:"primaryKey;index:idx_messages_created_id,priority:2"`
	Name      string         `json:"name" gorm:"size:100;not null"`
	Email     string         `json:"email" gorm:"size:200"`
	Content   string         `json:"content" gorm:"type:text;not null"`
	IP        string         `json:"ip" gorm:"size:45"`
	CreatedAt time.Time      `json:"create_at" gorm:"index:idx_messages_created_id,priority:1"`
	UpdatedAt time.Time      `json:"update_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Entity) TableName() string { return "messages" }

type CreateReq struct {
	Name    string `json:"name" binding:"omitempty,max=100"`
	Email   string `json:"email" binding:"omitempty,email,max=200"`
	Content string `json:"content" binding:"required,max=2000"`
}
