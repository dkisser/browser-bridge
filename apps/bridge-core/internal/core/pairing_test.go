package core

import (
	"errors"
	"regexp"
	"testing"
	"time"
)

// fixedNow returns a clock over a manually advanced time, playing the role
// of the `now` parameters in the TS pairing tests.
type testClock struct {
	now time.Time
}

func (c *testClock) time() time.Time { return c.now }
func (c *testClock) advance(d time.Duration) {
	c.now = c.now.Add(d)
}

func makeManager(t *testing.T) (*PairingManager, *testClock, func() string) {
	t.Helper()
	clock := &testClock{now: time.UnixMilli(1_700_000_000_000)}
	var hash string
	m := NewPairingManager(
		func() string { return hash },
		func(h string) error { hash = h; return nil },
		WithClock(clock.time),
	)
	return m, clock, func() string { return hash }
}

func TestStartReturnsCodeFromUnambiguousAlphabet(t *testing.T) {
	m, _, _ := makeManager(t)
	code, expiresIn := m.Start()
	if !regexp.MustCompile(`^[0-9A-HJ-NP-TV-Z]{8}$`).MatchString(code) {
		t.Fatalf("code %q does not match the Crockford alphabet shape", code)
	}
	if expiresIn != 5*60*1000 {
		t.Fatalf("expiresIn = %d, want 300000", expiresIn)
	}
}

func TestConfirmStoresOnlyTokenHash(t *testing.T) {
	m, _, storedHash := makeManager(t)
	code, _ := m.Start()
	result, err := m.Confirm(code)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if !result.OK {
		t.Fatal("confirm failed for the right code")
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(result.Token) {
		t.Fatalf("token %q is not 64 hex chars", result.Token)
	}
	if storedHash() != HashToken(result.Token) {
		t.Fatal("stored hash is not the SHA-256 of the token")
	}
	if !m.IsPaired() {
		t.Fatal("manager should report paired")
	}
}

func TestConfirmIsCaseInsensitiveAndToleratesDashes(t *testing.T) {
	m, _, _ := makeManager(t)
	code, _ := m.Start()
	mangled := ""
	for i, r := range code {
		if i > 0 {
			mangled += "-"
		}
		mangled += string(r)
	}
	result, err := m.Confirm(" " + mangled + " ")
	if err != nil || !result.OK {
		t.Fatalf("confirm with dashed/space-padded code: %+v, %v", result, err)
	}
	// lower-case variant of a fresh code
	m2, _, _ := makeManager(t)
	code2, _ := m2.Start()
	lowered := []byte(code2)
	for i, b := range lowered {
		if b >= 'A' && b <= 'Z' {
			lowered[i] = b + ('a' - 'A')
		}
	}
	if result, err := m2.Confirm(string(lowered)); err != nil || !result.OK {
		t.Fatalf("confirm with lower-case code: %+v, %v", result, err)
	}
}

func TestVerifyAcceptsPairedTokenAndRejectsOthers(t *testing.T) {
	m, _, _ := makeManager(t)
	code, _ := m.Start()
	result, err := m.Confirm(code)
	if err != nil || !result.OK {
		t.Fatalf("pairing failed: %+v, %v", result, err)
	}
	if !m.Verify(result.Token) {
		t.Fatal("verify rejected the paired token")
	}
	wrong := "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef"
	if m.Verify(wrong) {
		t.Fatal("verify accepted a wrong token")
	}
	if m.Verify("") {
		t.Fatal("verify accepted an empty token")
	}
}

func TestConfirmWithoutPendingCodeIsInvalid(t *testing.T) {
	m, _, _ := makeManager(t)
	result, err := m.Confirm("ABCDEFGH")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if result.Failure != ErrInvalidCode || result.AttemptsRemaining != nil {
		t.Fatalf("result = %+v, want invalid_code with no attemptsRemaining", result)
	}
}

func TestCodeExpiresAfterTTL(t *testing.T) {
	m, clock, _ := makeManager(t)
	code, _ := m.Start()
	clock.advance(5*time.Minute + time.Millisecond)
	result, err := m.Confirm(code)
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if result.Failure != ErrCodeExpired {
		t.Fatalf("result = %+v, want code_expired", result)
	}
}

func TestConfirmGivesUpAfterFiveWrongAttempts(t *testing.T) {
	m, _, _ := makeManager(t)
	code, _ := m.Start()
	for i := 1; i <= 4; i++ {
		result, err := m.Confirm("ZZZZZZZZ")
		if err != nil {
			t.Fatalf("confirm: %v", err)
		}
		if result.Failure != ErrInvalidCode {
			t.Fatalf("attempt %d: failure = %q, want invalid_code", i, result.Failure)
		}
		if result.AttemptsRemaining == nil || *result.AttemptsRemaining != 5-i {
			t.Fatalf("attempt %d: attemptsRemaining = %v, want %d", i, result.AttemptsRemaining, 5-i)
		}
	}
	fifth, err := m.Confirm("ZZZZZZZZ")
	if err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if fifth.Failure != ErrTooManyAttempts {
		t.Fatalf("fifth attempt: failure = %q, want too_many_attempts", fifth.Failure)
	}
	// Failure budget still exhausted — the right code no longer works either.
	if result, _ := m.Confirm(code); result.OK {
		t.Fatal("right code confirmed after budget exhaustion")
	}
}

func TestStartDoesNotResetFailureBudget(t *testing.T) {
	m, clock, _ := makeManager(t)
	m.Start()
	for i := 0; i < 4; i++ {
		if _, err := m.Confirm("ZZZZZZZZ"); err != nil {
			t.Fatalf("confirm: %v", err)
		}
	}
	// A fresh code must NOT reopen a full 5-try window.
	clock.advance(time.Second)
	m.Start()
	if result, _ := m.Confirm("ZZZZZZZZ"); result.Failure != ErrTooManyAttempts {
		t.Fatalf("result = %+v, want too_many_attempts", result)
	}
	// ...and the fresh code is locked out with it.
	clock.advance(time.Second)
	code, _ := m.Start()
	if result, _ := m.Confirm(code); result.Failure != ErrTooManyAttempts {
		t.Fatalf("result = %+v, want too_many_attempts", result)
	}
}

func TestFailuresExpireFromRollingWindow(t *testing.T) {
	m, clock, _ := makeManager(t)
	m.Start()
	for i := 0; i < 4; i++ {
		if _, err := m.Confirm("ZZZZZZZZ"); err != nil {
			t.Fatalf("confirm: %v", err)
		}
	}
	clock.advance(11 * time.Minute)
	code, _ := m.Start()
	if result, _ := m.Confirm(code); !result.OK {
		t.Fatalf("confirm after window expiry: %+v", result)
	}
}

func TestSuccessfulPairingClearsFailureBudget(t *testing.T) {
	m, clock, _ := makeManager(t)
	first, _ := m.Start()
	if _, err := m.Confirm("ZZZZZZZZ"); err != nil {
		t.Fatalf("confirm: %v", err)
	}
	if result, _ := m.Confirm(first); !result.OK {
		t.Fatalf("confirm first code: %+v", result)
	}
	// A later pairing round starts with a clean budget.
	clock.advance(time.Minute)
	second, _ := m.Start()
	for i := 1; i <= 4; i++ {
		result, _ := m.Confirm("YYYYYYYY")
		if result.AttemptsRemaining == nil || *result.AttemptsRemaining != 5-i {
			t.Fatalf("attempt %d: attemptsRemaining = %v, want %d", i, result.AttemptsRemaining, 5-i)
		}
	}
	if result, _ := m.Confirm(second); !result.OK {
		t.Fatalf("confirm second code: %+v", result)
	}
}

func TestRePairingRotatesTokenAndInvalidatesOld(t *testing.T) {
	m, _, _ := makeManager(t)
	code, _ := m.Start()
	first, err := m.Confirm(code)
	if err != nil || !first.OK {
		t.Fatalf("first pairing failed: %+v, %v", first, err)
	}
	code2, _ := m.Start()
	second, err := m.Confirm(code2)
	if err != nil || !second.OK {
		t.Fatalf("second pairing failed: %+v, %v", second, err)
	}
	if !m.Verify(second.Token) {
		t.Fatal("verify rejected the fresh token")
	}
	if m.Verify(first.Token) {
		t.Fatal("verify accepted the rotated-out token")
	}
}

func TestCodeNormalization(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		matches string
	}{
		{"exact", "0123ABCD", "0123ABCD"},
		{"lower case", "0123abcd", "0123ABCD"},
		{"dashes", "01-23-AB-CD", "0123ABCD"},
		{"spaces", "  0123ABCD ", "0123ABCD"},
		{"all combined", " 01-23-ab-cd\n", "0123ABCD"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !codesEqual(tt.input, tt.matches) {
				t.Fatalf("codesEqual(%q, %q) = false", tt.input, tt.matches)
			}
		})
	}
	if codesEqual("0123ABCE", "0123ABCD") {
		t.Fatal("codesEqual matched different codes")
	}
	if codesEqual("0123ABC", "0123ABCD") {
		t.Fatal("codesEqual matched different lengths")
	}
}

func TestConfirmPersistenceFailurePropagates(t *testing.T) {
	// TS: setTokenHash throws through the fetch handler → 500.
	m, _, _ := makeManager(t)
	code, _ := m.Start()
	injected := errors.New("injected")
	m.setTokenHash = func(string) error { return injected }
	if _, err := m.Confirm(code); !errors.Is(err, injected) {
		t.Fatalf("confirm error = %v, want the injected persistence error", err)
	}
}
