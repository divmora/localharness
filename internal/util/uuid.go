package util

import "github.com/google/uuid"

// NewUUID generates a time-sortable UUIDv7 string.
func NewUUID() string {
	return uuid.Must(uuid.NewV7()).String()
}
