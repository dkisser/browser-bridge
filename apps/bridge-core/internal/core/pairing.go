package core

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	// CodeLength is CODE_LENGTH in pairing.ts.
	CodeLength = 8
	// CodeTTL is CODE_TTL_MS (5 minutes).
	CodeTTL = 5 * time.Minute
	// MaxConfirmAttempts is MAX_CONFIRM_ATTEMPTS.
	MaxConfirmAttempts = 5
	// AttemptWindow is ATTEMPT_WINDOW_MS: failed confirmations count against a
	// rolling window that start() must not reset — otherwise alternating
	// start/confirm re-opens a fresh 5-try budget for every new code.
	AttemptWindow = 10 * time.Minute
)

// codeAlphabet is Crockford Base32: no I, L, O, U — unambiguous to read and
// type.
const codeAlphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

// Failure is the machine-readable pairing error (PairingError in pairing.ts).
type Failure string

const (
	ErrInvalidCode     Failure = "invalid_code"
	ErrCodeExpired     Failure = "code_expired"
	ErrTooManyAttempts Failure = "too_many_attempts"
)

// ConfirmResult mirrors PairConfirmResult in pairing.ts. AttemptsRemaining is
// nil unless the TS result carries the field (wrong code with budget left).
type ConfirmResult struct {
	OK                bool
	Token             string
	Failure           Failure
	AttemptsRemaining *int
}

type pendingCode struct {
	code      string
	expiresAt time.Time
}

// HashToken is hashToken in pairing.ts: hex SHA-256 of the token.
func HashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

// PairingOption customizes a PairingManager.
type PairingOption func(*PairingManager)

// WithClock overrides the time source (tests).
func WithClock(now func() time.Time) PairingOption {
	return func(m *PairingManager) { m.now = now }
}

// PairingManager runs the pairing handshake — the Go port of src/pairing.ts:
// Crockford Base32 pairing codes with a 5-minute TTL, a rolling 5-attempt
// failure window that Start must not reset, and SHA-256-hashed bearer tokens
// compared in constant time. Token persistence is delegated to the caller
// (StateManager in production) exactly as the TS constructor takes
// getTokenHash/setTokenHash closures.
type PairingManager struct {
	mu           sync.Mutex
	getTokenHash func() string
	setTokenHash func(string) error
	now          func() time.Time

	pending *pendingCode
	// Timestamps of failed confirmations inside the rolling window. start()
	// intentionally does NOT clear this: the failure budget is per-window, not
	// per-code.
	failures []time.Time
}

func NewPairingManager(getTokenHash func() string, setTokenHash func(string) error, opts ...PairingOption) *PairingManager {
	m := &PairingManager{
		getTokenHash: getTokenHash,
		setTokenHash: setTokenHash,
		now:          time.Now,
	}
	for _, opt := range opts {
		opt(m)
	}
	return m
}

func (m *PairingManager) IsPaired() bool {
	return m.getTokenHash() != ""
}

// Start issues a fresh code, replacing any pending one. expiresIn is
// milliseconds, matching the TS response shape.
func (m *PairingManager) Start() (code string, expiresIn int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	code = randomCode()
	m.pending = &pendingCode{code: code, expiresAt: m.now().Add(CodeTTL)}
	return code, int(CodeTTL / time.Millisecond)
}

func randomCode() string {
	var b [CodeLength]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	var code [CodeLength]byte
	for i, v := range b {
		// len(codeAlphabet) is 32 and 256 % 32 == 0, so the modulo is uniform
		// (Node's randomInt is likewise uniform).
		code[i] = codeAlphabet[int(v)%len(codeAlphabet)]
	}
	return string(code[:])
}

// Confirm checks a code against the pending one. A persistence failure from
// setTokenHash is returned as error (the TS original throws through the HTTP
// handler, which Bun turns into a 500).
func (m *PairingManager) Confirm(code string) (ConfirmResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()

	pending := m.pending
	if pending == nil {
		return ConfirmResult{Failure: ErrInvalidCode}, nil
	}
	if now.After(pending.expiresAt) {
		m.pending = nil
		return ConfirmResult{Failure: ErrCodeExpired}, nil
	}
	kept := m.failures[:0]
	for _, at := range m.failures {
		if now.Sub(at) < AttemptWindow {
			kept = append(kept, at)
		}
	}
	m.failures = kept
	if len(m.failures) >= MaxConfirmAttempts {
		return ConfirmResult{Failure: ErrTooManyAttempts}, nil
	}
	if !codesEqual(code, pending.code) {
		m.failures = append(m.failures, now)
		if len(m.failures) >= MaxConfirmAttempts {
			return ConfirmResult{Failure: ErrTooManyAttempts}, nil
		}
		remaining := MaxConfirmAttempts - len(m.failures)
		return ConfirmResult{Failure: ErrInvalidCode, AttemptsRemaining: &remaining}, nil
	}
	token, err := randomToken()
	if err != nil {
		return ConfirmResult{}, err
	}
	if err := m.setTokenHash(HashToken(token)); err != nil {
		return ConfirmResult{}, fmt.Errorf("persist token hash: %w", err)
	}
	m.pending = nil
	// A successful pairing clears the failure budget for the next round.
	m.failures = nil
	return ConfirmResult{OK: true, Token: token}, nil
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

// Verify checks a bearer token against the stored hash in constant time.
func (m *PairingManager) Verify(token string) bool {
	expectedHex := m.getTokenHash()
	if expectedHex == "" || token == "" {
		return false
	}
	actual := sha256.Sum256([]byte(token))
	expected, err := hex.DecodeString(expectedHex)
	if err != nil {
		return false
	}
	// Length must match before timingSafeEqual / ConstantTimeCompare: a
	// truncated or corrupted stored hash (len != 32 bytes) would otherwise
	// run ConstantTimeCompare over actual[:] (32 bytes) anyway, leaking a
	// timing signal that the stored hash is shorter than the SHA-256
	// actual. The TS pair-store does the same length guard before its
	// timingSafeEqual call; mirror that discipline.
	if len(expected) != len(actual) {
		return false
	}
	return subtle.ConstantTimeCompare(actual[:], expected) == 1
}

func normalizeCode(code string) string {
	return strings.TrimSpace(strings.ReplaceAll(strings.ToUpper(code), "-", ""))
}

// codesEqual is constant-time, matching the discipline of Verify: the
// pairing code is a bearer secret while it lives, so its check must not
// short-circuit on the first differing character.
func codesEqual(input, expected string) bool {
	return subtle.ConstantTimeCompare([]byte(normalizeCode(input)), []byte(expected)) == 1
}
