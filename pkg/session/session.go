// Package session provides session management for MTProto.
package session

import (
	"sync"
	"time"
)

// Session represents an MTProto session.
type Session struct {
	ID        int64
	AuthKeyID int64
	Layer     int
	UserID    int64
	Data      map[string]interface{}

	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewSession creates a new session.
func NewSession(authKeyID int64) *Session {
	now := time.Now()
	return &Session{
		ID:        generateID(),
		AuthKeyID: authKeyID,
		Data:      make(map[string]interface{}),
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// UpdateTimestamp updates the session's last update time.
func (s *Session) UpdateTimestamp() {
	s.UpdatedAt = time.Now()
}

// SetData sets session data.
func (s *Session) SetData(key string, value interface{}) {
	s.Data[key] = value
	s.UpdateTimestamp()
}

// GetData gets session data.
func (s *Session) GetData(key string) interface{} {
	return s.Data[key]
}

// generateID generates a unique session ID.
func generateID() int64 {
	// TODO: Implement proper ID generation
	return time.Now().UnixNano()
}