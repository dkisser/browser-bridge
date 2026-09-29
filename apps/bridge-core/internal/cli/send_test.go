package cli

import (
	"testing"
	"time"

	"browser-bridge/internal/core"
)

// responseError decides what the user reads when a command fails. The
// message-first path is the one that runs today, and it must not move: the
// extension already sends a human-readable `message` alongside every policy
// rejection, and rewriting it here would change output nobody asked to change.
//
// The structured `denied` branch is the fallback for a producer that omits
// the message. It does not fire against today's extension, which is exactly
// why it needs a test — otherwise it is a branch that only ever runs when
// nobody is looking.
func TestResponseError(t *testing.T) {
	tests := []struct {
		name     string
		payload  core.ResponsePayload
		fallback string
		want     string
	}{
		{
			// Every competing source of truth present at once: the message
			// the extension authored, the bare reason code, and a fully
			// populated structured denial that would render a *different*
			// string. Without the denial in the payload this case cannot
			// tell "message wins" from "message wins because nothing else
			// was on offer" — which is the opposite of what it claims.
			name: "message wins over everything",
			payload: core.ResponsePayload{
				Status:  "error",
				Message: "human text",
				Error:   "origin_not_approved",
				Denied: &core.Denial{
					Reason:     "origin_not_approved",
					Command:    "click",
					Origin:     "https://example.com",
					Capability: "submit",
				},
			},
			fallback: "fb",
			want:     "human text",
		},
		{
			name:     "error when there is no message",
			payload:  core.ResponsePayload{Status: "error", Error: "sw_timeout"},
			fallback: "fb",
			want:     "sw_timeout",
		},
		{
			name:     "fallback when the payload says nothing",
			payload:  core.ResponsePayload{Status: "error"},
			fallback: "fb",
			want:     "fb",
		},
		{
			name: "denial with origin and capability",
			payload: core.ResponsePayload{
				Status: "error",
				Error:  "approval_required",
				Denied: &core.Denial{
					Reason:     "approval_required",
					Command:    "click",
					Origin:     "https://example.com",
					Capability: "submit",
				},
			},
			fallback: "fb",
			want:     "Refused: https://example.com (approval_required, capability submit).",
		},
		{
			name: "denial with origin but no capability",
			payload: core.ResponsePayload{
				Status: "error",
				Denied: &core.Denial{
					Reason:  "origin_not_approved",
					Command: "navigate",
					Origin:  "https://example.com",
				},
			},
			fallback: "fb",
			want:     "Refused: https://example.com (origin_not_approved).",
		},
		{
			name: "denial with neither origin nor capability falls back to the command",
			payload: core.ResponsePayload{
				Status: "error",
				Denied: &core.Denial{Reason: "human_assist_active", Command: "click"},
			},
			fallback: "fb",
			want:     "Refused: click (human_assist_active).",
		},
		{
			name:     "denial with no command at all still says something",
			payload:  core.ResponsePayload{Status: "error", Denied: &core.Denial{Reason: "origin_blocked"}},
			fallback: "fb",
			want:     "Refused: policy (origin_blocked).",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := responseError(tt.payload, tt.fallback); got != tt.want {
				t.Errorf("responseError = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestResponseErrorDenialOutranksTheBareError pins the one case where the
// structured denial is consulted at all: there is no message, and the bare
// reason code alone would leave the user with no idea what was refused.
func TestResponseErrorDenialOutranksTheBareError(t *testing.T) {
	withDenial := core.ResponsePayload{
		Status: "error",
		Error:  "origin_not_approved",
		Denied: &core.Denial{
			Reason:  "origin_not_approved",
			Command: "click",
			Origin:  "https://example.com",
		},
	}
	if got, want := responseError(withDenial, "fb"), "Refused: https://example.com (origin_not_approved)."; got != want {
		t.Errorf("responseError = %q, want %q", got, want)
	}
	without := withDenial
	without.Denied = nil
	if got, want := responseError(without, "fb"), "origin_not_approved"; got != want {
		t.Errorf("without a denial = %q, want the bare reason code %q", got, want)
	}
}

// cliTransportTimeout is the whole point of giving the CLI's navigate an
// in-page budget: without the slack the CLI's own timer and the extension's
// page wait expire together, and the user is told the control plane stopped
// responding instead of being shown the extension's diagnostic. These cases
// are the only thing pinning it — the mutation "return the plain --timeout"
// is invisible without them.
func TestCLITransportTimeout(t *testing.T) {
	tests := []struct {
		name    string
		timeout int // --timeout, milliseconds
		params  map[string]any
		want    time.Duration
	}{
		{
			name:    "no in-page budget is just --timeout",
			timeout: 10000,
			params:  map[string]any{"url": "https://example.com"},
			want:    10 * time.Second,
		},
		{
			name:    "navigate's in-page budget outlasts the default --timeout",
			timeout: 10000,
			params:  map[string]any{"url": "https://example.com", "timeout": 10000},
			want:    10*time.Second + 500*time.Millisecond,
		},
		{
			name:    "a long budget is honoured",
			timeout: 60000,
			params:  map[string]any{"url": "https://example.com", "timeout": 60000},
			want:    60*time.Second + 500*time.Millisecond,
		},
		{
			name:    "the in-page budget wins when it is the longer of the two",
			timeout: 200,
			params:  map[string]any{"selector": "#a", "timeout": 60000},
			want:    60*time.Second + 500*time.Millisecond,
		},
		{
			name:    "a long --timeout is still honoured",
			timeout: 90000,
			params:  map[string]any{"url": "https://example.com", "timeout": 10000},
			want:    90 * time.Second,
		},
		{
			name:    "wait:navigation's own budget drives it",
			timeout: 10000,
			params:  map[string]any{"timeout": 45000},
			want:    45*time.Second + 500*time.Millisecond,
		},
		{
			name:    "a non-numeric timeout is ignored",
			timeout: 10000,
			params:  map[string]any{"timeout": "30000"},
			want:    10 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := cliTransportTimeout(&globals{timeout: tt.timeout}, tt.params)
			if got != tt.want {
				t.Errorf("cliTransportTimeout = %v, want %v", got, tt.want)
			}
		})
	}
}

// The two cases the function exists to distinguish: tied budgets lose the
// extension's diagnostic, slack budgets keep it.
func TestCLITransportTimeoutNeverTiesTheExtension(t *testing.T) {
	g := &globals{timeout: 30000}
	params := map[string]any{"url": "https://example.com", "timeout": 30000}

	got := cliTransportTimeout(g, params)
	if got <= 30*time.Second {
		t.Fatalf("transport deadline %v does not outlast the extension's 30s "+
			"in-page budget; the CLI would give up first and report a timeout "+
			"for a control plane that was working", got)
	}
}
