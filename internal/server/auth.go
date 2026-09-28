package server

import (
	"net/http"
)

// AuthMiddleware wraps an http.HandlerFunc to require a matching API key.
// If expectedKey is empty, authentication is bypassed.
// Accepts key via x-localharness-api-key header or ?api_key= / ?key= query parameters.
func AuthMiddleware(expectedKey string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if expectedKey != "" {
			clientKey := r.Header.Get("x-localharness-api-key")
			if clientKey == "" {
				clientKey = r.URL.Query().Get("api_key")
			}
			if clientKey == "" {
				clientKey = r.URL.Query().Get("key")
			}
			if clientKey != expectedKey {
				http.Error(w, "Unauthorized: invalid or missing API key", http.StatusUnauthorized)
				return
			}
		}
		next(w, r)
	}
}
