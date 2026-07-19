// Package auth signs and verifies lightweight bearer tokens.
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
)

// Manager creates and validates authentication tokens.
type Manager struct {
	secret []byte
}

// NewManager creates a token manager with a random process-local secret.
func NewManager() (*Manager, error) {
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return nil, err
	}
	return &Manager{secret: secret}, nil
}

// Issue returns a signed bearer token for userID.
func (m *Manager) Issue(userID int64) string {
	payload := strconv.FormatInt(userID, 10)
	signature := m.sign(payload)
	return payload + "." + signature
}

// Verify validates token and returns the encoded user ID.
func (m *Manager) Verify(token string) (int64, bool) {
	payload, signature, ok := strings.Cut(strings.TrimSpace(token), ".")
	if !ok || payload == "" || signature == "" {
		return 0, false
	}

	if !hmac.Equal([]byte(signature), []byte(m.sign(payload))) {
		return 0, false
	}

	userID, err := strconv.ParseInt(payload, 10, 64)
	if err != nil || userID <= 0 {
		return 0, false
	}

	return userID, true
}

func (m *Manager) sign(payload string) string {
	mac := hmac.New(sha256.New, m.secret)
	_, _ = fmt.Fprint(mac, payload)
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
