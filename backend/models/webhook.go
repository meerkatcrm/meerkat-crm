package models

import (
	"time"

	"gorm.io/gorm"
)

type Webhook struct {
	gorm.Model
	UserID   uint     `gorm:"not null;index"`
	Name     string   `gorm:"not null"`
	URL      string   `gorm:"not null"`
	Events   []string `gorm:"type:text;serializer:json"`
	Secret   string   `gorm:"not null"`
	IsActive bool     // no gorm default tag: GORM would replace false with it on create
}

type WebhookDelivery struct {
	gorm.Model
	WebhookID   uint   `gorm:"not null;index"`
	EventType   string `gorm:"not null"`
	Payload     string `gorm:"not null"`
	StatusCode  *int
	Error       *string
	Attempts    int
	NextRetryAt *time.Time `gorm:"index:idx_webhook_deliveries_status_retry,priority:2"`
	// Status is the outbox state: pending (waiting for the first try or a retry), delivered
	// or dead (gave up after the last try or a permanent refusal).
	Status   string `gorm:"not null;default:delivered;index:idx_webhook_deliveries_status_retry,priority:1"`
	LockedAt *time.Time
}

// Outbox states shared by webhook deliveries and the email outbox.
const (
	OutboxPending   = "pending"
	OutboxDelivered = "delivered"
	OutboxDead      = "dead"
)

// EmailOutbox is a queued email. The row is the source of truth: it is written before
// anything is sent, so a restart or a crash does not lose the message, and a failing
// mail server is retried with backoff until the row is dead.
type EmailOutbox struct {
	ID            uint      `gorm:"primarykey"`
	UserID        *uint     `gorm:"index"`
	ToAddr        string    `gorm:"not null"`
	Subject       string    `gorm:"not null"`
	HTML          string    `gorm:"not null"`
	Status        string    `gorm:"not null;default:pending"`
	Attempts      int       `gorm:"not null;default:0"`
	NextAttemptAt time.Time `gorm:"not null"`
	// ExpiresAt (optional): after this the email is not sent any more (password reset links).
	ExpiresAt *time.Time
	LockedAt  *time.Time
	LastError string `gorm:"not null;default:''"`
	SentAt    *time.Time
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (EmailOutbox) TableName() string { return "email_outbox" }
