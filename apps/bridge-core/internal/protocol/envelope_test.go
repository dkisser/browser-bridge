package protocol

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

var uuidV4Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

func TestEncode(t *testing.T) {
	tests := []struct {
		name        string
		typ         Type
		payload     json.RawMessage
		id          string
		browserID   string
		wantID      string // empty means "must be a generated UUIDv4"
		wantPayload bool
	}{
		{
			name:        "command with explicit id and payload",
			typ:         TypeCommand,
			payload:     json.RawMessage(`{"command":"pageinfo","tabId":1}`),
			id:          "fixed-id",
			browserID:   "browser-1",
			wantID:      "fixed-id",
			wantPayload: true,
		},
		{
			name:        "event without payload omits the key (TS parity)",
			typ:         TypeEvent,
			browserID:   "browser-1",
			wantPayload: false,
		},
		{
			name:        "response keeps null-ish payloads absent too",
			typ:         TypeResponse,
			payload:     nil,
			id:          "resp-1",
			wantID:      "resp-1",
			wantPayload: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			raw, err := Encode(tt.typ, tt.payload, tt.id, tt.browserID)
			if err != nil {
				t.Fatalf("Encode: %v", err)
			}

			var got map[string]json.RawMessage
			if err := json.Unmarshal([]byte(raw), &got); err != nil {
				t.Fatalf("encoded output is not valid JSON: %v", err)
			}

			id := strings.Trim(string(got["id"]), `"`)
			if tt.wantID != "" && id != tt.wantID {
				t.Errorf("id = %q; want %q", id, tt.wantID)
			}
			if tt.wantID == "" && !uuidV4Pattern.MatchString(id) {
				t.Errorf("generated id %q is not a UUIDv4", id)
			}

			if string(got["type"]) != `"`+string(tt.typ)+`"` {
				t.Errorf("type = %s; want %q", got["type"], tt.typ)
			}

			_, hasPayload := got["payload"]
			if hasPayload != tt.wantPayload {
				t.Errorf("payload key present = %v; want %v", hasPayload, tt.wantPayload)
			}

			var ts int64
			if err := json.Unmarshal(got["timestamp"], &ts); err != nil {
				t.Fatalf("timestamp is not a number: %v", err)
			}
			if d := time.Since(time.UnixMilli(ts)); d < 0 || d > time.Minute {
				t.Errorf("timestamp %d is not within the last minute", ts)
			}
		})
	}
}

func TestDecode(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    Envelope
		wantErr bool
	}{
		{
			name: "full envelope",
			raw:  `{"id":"1","type":"command","browserId":"b","payload":{"command":"pageinfo"},"timestamp":123}`,
			want: Envelope{
				ID:        "1",
				Type:      TypeCommand,
				BrowserID: "b",
				Payload:   json.RawMessage(`{"command":"pageinfo"}`),
				Timestamp: 123,
			},
		},
		{
			name: "unknown fields ignored (TS cast parity)",
			raw:  `{"id":"2","type":"event","browserId":"","timestamp":5,"futureField":true}`,
			want: Envelope{ID: "2", Type: TypeEvent, Timestamp: 5},
		},
		{
			name:    "invalid JSON",
			raw:     `{nope`,
			wantErr: true,
		},
		{
			name:    "empty input",
			raw:     "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Decode(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got envelope %+v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if got.ID != tt.want.ID || got.Type != tt.want.Type ||
				got.BrowserID != tt.want.BrowserID || got.Timestamp != tt.want.Timestamp ||
				string(got.Payload) != string(tt.want.Payload) {
				t.Errorf("got %+v; want %+v", got, tt.want)
			}
		})
	}
}
