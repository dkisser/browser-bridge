package cli

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakePairServer answers /api/pair/start with the given status and body.
func fakePairServer(t *testing.T, status int, body string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/pair/start" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestPairSuccess(t *testing.T) {
	local := fakePairServer(t, http.StatusOK, `{"success":true,"data":{"code":"ABCD1234","expiresIn":300000}}`)
	stdout, stderr, err := runCLI(t, "pair", "--local", local)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	want := "\n" +
		"  Pairing code:  ABCD1234\n" +
		"\n" +
		"  Enter this code in the Browser Bridge side panel to pair \n" +
		"  The code is valid for 5 minutes. Re-running this command generates a new code and invalidates the previous one.\n" +
		"\n"
	if stdout != want {
		t.Errorf("stdout = %q, want %q", stdout, want)
	}
	if stderr != "" {
		t.Errorf("stderr = %q, want empty", stderr)
	}
}

func TestPairFailure(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
		want   string
	}{
		{
			name:   "http error",
			status: http.StatusInternalServerError,
			body:   `{"success":false,"error":"boom"}`,
			want:   "Pairing request failed: HTTP 500\n",
		},
		{
			name:   "unsuccessful body",
			status: http.StatusOK,
			body:   `{"success":false,"error":"pairing_disabled"}`,
			want:   "Pairing request failed: pairing_disabled\n",
		},
		{
			name:   "unsuccessful without error field",
			status: http.StatusOK,
			body:   `{"success":false}`,
			want:   "Pairing request failed: unknown error\n",
		},
		{
			name:   "success without data",
			status: http.StatusOK,
			body:   `{"success":true}`,
			want:   "Pairing request failed: unknown error\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			local := fakePairServer(t, tt.status, tt.body)
			stdout, stderr, err := runCLI(t, "pair", "--local", local)
			if !errors.Is(err, ErrReported) {
				t.Fatalf("err = %v, want ErrReported", err)
			}
			if stderr != tt.want {
				t.Errorf("stderr = %q, want %q", stderr, tt.want)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want empty", stdout)
			}
		})
	}
}

func TestPairUnreachable(t *testing.T) {
	_, stderr, err := runCLI(t, "pair", "--local", "http://127.0.0.1:1")
	if !errors.Is(err, ErrReported) {
		t.Fatalf("err = %v, want ErrReported", err)
	}
	want := "Could not reach the local proxy at http://127.0.0.1:1.\n" +
		"Is the service running? Start it with: bridge service up\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
}
