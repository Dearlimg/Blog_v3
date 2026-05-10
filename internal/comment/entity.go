package comment

import (
	"time"

	"gorm.io/gorm"
)

type Entity struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	UserID    uint           `json:"user_id" gorm:"index;not null"`
	Username  string         `json:"username" gorm:"-"`
	Content   string         `json:"content" gorm:"type:text;not null"`
	ParentID  *uint          `json:"parent_id" gorm:"index"`
	Replies   []Entity       `json:"replies" gorm:"foreignkey:ParentID"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

type CreateReq struct {
	UserID   uint   `json:"user_id" binding:"required"`
	Content  string `json:"content" binding:"required"`
	ParentID *uint  `json:"parent_id"`
}

type UpdateReq struct {
	Content string `json:"content" binding:"required"`
}
