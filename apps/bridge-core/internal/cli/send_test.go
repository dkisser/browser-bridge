package cli

import (
	"testing"

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
			name:     "message wins over everything",
			payload:  core.ResponsePayload{Status: "error", Message: "human text", Error: "origin_not_approved"},
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
