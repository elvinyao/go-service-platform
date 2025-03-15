package model

import (
	"time"
)

// Badge represents a user achievement or recognition
type Badge struct {
	ID          string                 `json:"id"`
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	UserID      string                 `json:"user_id"`
	AwardedAt   time.Time              `json:"awarded_at"`
	Type        string                 `json:"type"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Revoked     bool                   `json:"revoked"`
	RevokedAt   time.Time              `json:"revoked_at,omitempty"`
}
