package auth

import "github.com/golang-jwt/jwt/v5"

// Claims represents the JWT claims for access tokens per ADR-015 §3.3.
type Claims struct {
	jwt.RegisteredClaims
	SessionID string `json:"sid"`
	Scope     string `json:"scope"`
}
