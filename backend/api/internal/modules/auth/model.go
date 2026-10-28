package auth

import "time"

type User struct {
	ID           string    `gorm:"type:char(36);primaryKey"`
	Email        string    `gorm:"type:varchar(320);uniqueIndex;not null"`
	PasswordHash string    `gorm:"type:varchar(255);not null"`
	CreatedAt    time.Time `gorm:"not null"`
	UpdatedAt    time.Time `gorm:"not null"`
}

func (User) TableName() string { return "users" }
