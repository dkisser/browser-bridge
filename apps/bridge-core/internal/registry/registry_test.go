package registry

import (
	"encoding/json"
	"testing"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
)

func TestSetStatusCreatesEntryLazily(t *testing.T) {
	r := New()
	// The bridge-core merge dropped the explicit register handshake: the
	// router writes the entry on the first status update.
	if !r.SetStatus("b-1", protocol.StatusOnline) {
		t.Fatal("SetStatus returned false")
	}
	status, known := r.GetStatus("b-1")
	if !known || status != protocol.StatusOnline {
		t.Fatalf("GetStatus = %q, %v", status, known)
	}

	browsers := r.ListBrowsers()
	if len(browsers) != 1 {
		t.Fatalf("ListBrowsers = %v", browsers)
	}
	b := browsers[0]
	if b.BrowserID != "b-1" || b.UserID != "extension" || b.Status != protocol.StatusOnline || b.LastSeen == 0 {
		t.Fatalf("entry = %+v", b)
	}
}

func TestUnknownBrowserIsNotKnown(t *testing.T) {
	r := New()
	if _, known := r.GetStatus("b-nope"); known {
		t.Fatal("unknown browser reported as known")
	}
}

func TestSetStatusUpdatesExistingEntry(t *testing.T) {
	r := New()
	r.SetStatus("b-1", protocol.StatusOnline)
	r.SetStatus("b-1", protocol.StatusOffline)
	status, _ := r.GetStatus("b-1")
	if status != protocol.StatusOffline {
		t.Fatalf("status = %q, want offline", status)
	}
	if len(r.ListBrowsers()) != 1 {
		t.Fatal("update created a duplicate entry")
	}
}

func TestListBrowsersShapeAndOrder(t *testing.T) {
	r := New()
	r.SetStatus("b-1", protocol.StatusOnline)
	r.SetStatus("b-2", protocol.StatusOffline)

	data, err := json.Marshal(r.ListBrowsers())
	if err != nil {
		t.Fatal(err)
	}
	// TS: Array.from(map.values()) — insertion order, and the ws field is
	// destructured out of the wire shape.
	var decoded []map[string]any
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if len(decoded) != 2 || decoded[0]["browserId"] != "b-1" || decoded[1]["browserId"] != "b-2" {
		t.Fatalf("list = %s", data)
	}
	if _, ok := decoded[0]["ws"]; ok {
		t.Fatalf("ws leaked into the wire shape: %s", data)
	}
}

func TestListBrowsersEmptyIsJSONArray(t *testing.T) {
	r := New()
	data, err := json.Marshal(r.ListBrowsers())
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "[]" {
		t.Fatalf("empty list = %s, want []", data)
	}
}
