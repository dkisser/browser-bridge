package inbound

import (
	"strconv"
	"strings"
)

// AuthResult mirrors AuthResult in packages/shared/src/auth.ts.
type AuthResult struct {
	Valid  bool
	UserID string
}

// Authorizer mirrors AuthProvider in packages/shared/src/auth.ts. Only
// validateHeader is ported: it is the sole method the inbound server calls.
type Authorizer interface {
	ValidateHeader(authorizationHeader string) AuthResult
}

// NoopAuthorizer is NoopAuthProvider: loopback default, everything valid.
type NoopAuthorizer struct{}

func (NoopAuthorizer) ValidateHeader(string) AuthResult {
	return AuthResult{Valid: true, UserID: "local"}
}

// APIKeyAuthorizer is ApiKeyAuthProvider constructed from a plain key list:
// keys map to synthetic user ids user-0, user-1, ... in order.
type APIKeyAuthorizer struct {
	validKeys map[string]string
}

func NewAPIKeyAuthorizer(keys []string) *APIKeyAuthorizer {
	valid := make(map[string]string, len(keys))
	for i, key := range keys {
		valid[key] = "user-" + strconv.Itoa(i)
	}
	return &APIKeyAuthorizer{validKeys: valid}
}

func (a *APIKeyAuthorizer) ValidateToken(token string) AuthResult {
	userID, ok := a.validKeys[token]
	if !ok {
		return AuthResult{Valid: false, UserID: ""}
	}
	return AuthResult{Valid: true, UserID: userID}
}

func (a *APIKeyAuthorizer) ValidateHeader(authorizationHeader string) AuthResult {
	if !strings.HasPrefix(authorizationHeader, "Bearer ") {
		return AuthResult{Valid: false, UserID: ""}
	}
	return a.ValidateToken(authorizationHeader[len("Bearer "):])
}
