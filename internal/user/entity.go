package user

import (
	"time"

	"gorm.io/gorm"
)

type Entity struct {
	ID        uint           `json:"id" gorm:"primaryKey"`
	Username  string         `json:"username" gorm:"uniqueIndex;size:100;not null"`
	Email     string         `json:"email" gorm:"uniqueIndex;size:200;not null"`
	Password  string         `json:"-" gorm:"not null"`
	Verified  bool           `json:"verified" gorm:"default:false"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (Entity) TableName() string { return "users" }

type LoginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type RegisterReq struct {
	Username string `json:"username" binding:"required,min=3,max=50"`
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required,min=6"`
}

type SendCodeReq struct {
	Email string `json:"email" binding:"required,email"`
}

type VerifyEmailReq struct {
	Email string `json:"email" binding:"required,email"`
	Code  string `json:"code" binding:"required"`
}

type LoginResp struct {
	Token string  `json:"token"`
	User  *Entity `json:"user"`
}
