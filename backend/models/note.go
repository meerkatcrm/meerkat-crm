package models

import (
	"time"

	"gorm.io/gorm"
)

// Note struct to represent notes attached to a contact
type Note struct {
	gorm.Model
	UserID    uint      `gorm:"not null;index" json:"-"`
	Content   string    `json:"content" validate:"required,min=1,max=5000"`
	Date      time.Time `json:"date" validate:"required"`
	Image     string    `gorm:"not null;default:''" json:"image"` // set only through the image endpoints
	ContactID *uint     `json:"contact_id" validate:"required"`
	Contact   Contact   `gorm:"constraint:OnUpdate:CASCADE,OnDelete:CASCADE;" json:"contact,omitempty"`
}
