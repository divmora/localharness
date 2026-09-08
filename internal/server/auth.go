package server

import (
	"net/http"
)

// AuthMiddleware wraps an http.HandlerFunc to require a matching API key.
// If expectedKey is empty, authentication is bypassed.
func AuthMiddleware(expectedKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if expectedKey != "" {
			clientKey := r.Header.Get("x-localharness-api-key")
			if clientKey != expectedKey {
				http.Error(w, "Unauthorized: invalid or missing API key", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}
