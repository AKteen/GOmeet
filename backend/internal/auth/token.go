package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

const algorithm = "HS256"

// HashPassword hashes a password with bcrypt.
func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// VerifyPassword reports whether plain matches the bcrypt hash.
func VerifyPassword(plain, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(plain)) == nil
}

// CreateAccessToken signs an HS256 JWT. expiresDelta overrides the minute lifetime, including negative values.
func CreateAccessToken(secret, subject string, expireMinutes int, expiresDelta *time.Duration) (string, error) {
	lifetime := time.Duration(expireMinutes) * time.Minute
	if expiresDelta != nil {
		lifetime = *expiresDelta
	}
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		ExpiresAt: jwt.NewNumericDate(time.Now().UTC().Add(lifetime)),
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

// SubjectFromToken returns the sub claim or an error when the token is invalid or expired.
func SubjectFromToken(secret, tokenString string) (string, error) {
	claims := &jwt.RegisteredClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(t *jwt.Token) (any, error) {
		if t.Method == nil || t.Method.Alg() != algorithm {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})
	if err != nil || !token.Valid {
		return "", errors.New("invalid or expired token")
	}
	if claims.Subject == "" {
		return "", errors.New("invalid token")
	}
	return claims.Subject, nil
}
