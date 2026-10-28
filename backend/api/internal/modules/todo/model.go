package todo

import "time"

type Todo struct {
	ID        string    `gorm:"type:char(36);primaryKey"`
	UserID    string    `gorm:"type:char(36);not null;index"`
	Title     string    `gorm:"type:varchar(200);not null"`
	Completed bool      `gorm:"not null"`
	CreatedAt time.Time `gorm:"not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (Todo) TableName() string { return "todos" }
