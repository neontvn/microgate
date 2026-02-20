package middleware

import (
	"net/http"
	"strings"

	"github.com/golang-jwt/jwt/v5"
)

// Auth holds valid API keys and the JWT signing secret.
type Auth struct {
	apiKeys   map[string]bool
	jwtSecret []byte
}

// NewAuth creates an Auth middleware with the given API keys and JWT secret.
func NewAuth(apiKeys []string, jwtSecret string) *Auth {
	keys := make(map[string]bool)
	for _, k := range apiKeys {
		keys[k] = true
	}
	return &Auth{
		apiKeys:   keys,
		jwtSecret: []byte(jwtSecret),
	}
}

// Middleware returns the auth Middleware.
// Checks X-API-Key header first, then falls back to Authorization: Bearer <JWT>.
// If neither is valid, returns 401 Unauthorized.
func (a *Auth) Middleware() Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Check API key first
			if key := r.Header.Get("X-API-Key"); key != "" {
				if a.apiKeys[key] {
					next.ServeHTTP(w, r)
					return
				}
				http.Error(w, "Invalid API Key", http.StatusUnauthorized)
				return
			}

			// Fall back to JWT Bearer token
			authHeader := r.Header.Get("Authorization")
			if authHeader == "" {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			// Extract token from "Bearer <token>"
			tokenString := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenString == authHeader {
				// No "Bearer " prefix found
				http.Error(w, "Invalid Authorization Header", http.StatusUnauthorized)
				return
			}

			// Parse and validate the JWT
			token, err := jwt.Parse(tokenString, func(token *jwt.Token) (interface{}, error) {
				// keyFunc returns the secret used to verify the token's signature
				return a.jwtSecret, nil
			})

			if err != nil || !token.Valid {
				http.Error(w, "Invalid Token", http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
