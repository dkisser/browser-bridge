package state

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"

	"github.com/dkisser/browser-bridge/apps/bridge-core/internal/protocol"
)

func newManagerInDir(t *testing.T, dir string, opts ...Option) *Manager {
	t.Helper()
	t.Setenv("BB_HOME", dir)
	m, err := NewManager(opts...)
	if err != nil {
		t.Fatalf("NewManager: %v", err)
	}
	return m
}

func TestWritesConfigUnderBBHome(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir)

	if !regexp.MustCompile(`^b-[0-9a-f]{8}$`).MatchString(m.BrowserID()) {
		t.Fatalf("browserId %q does not match b-<8 hex>", m.BrowserID())
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json")); err != nil {
		t.Fatalf("config.json not written: %v", err)
	}
}

func TestConfigFilePermissionsAre0600(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir)
	if err := m.SetExtensionTokenHash("hash"); err != nil {
		t.Fatalf("SetExtensionTokenHash: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config.json perm = %o, want 600", perm)
	}
}

func TestLoadTightensPermissionsOnExistingConfig(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	if err := os.WriteFile(file, []byte(`{"browserId":"b-keepme"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	newManagerInDir(t, dir)
	info, err := os.Stat(file)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config.json perm after load = %o, want 600", perm)
	}
}

func TestReusesBrowserIDFromExistingConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"browserId":"b-keepme"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newManagerInDir(t, dir)
	if m.BrowserID() != "b-keepme" {
		t.Fatalf("browserId = %q, want b-keepme", m.BrowserID())
	}
}

func TestDoesNotReserializeGhostFields(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "config.json")
	ghost := `{"browserId":"b-ghosty","apiToken":"ghost-token","serverUrl":"ws://ghost"}`
	if err := os.WriteFile(file, []byte(ghost), 0o600); err != nil {
		t.Fatal(err)
	}

	m := newManagerInDir(t, dir)
	if err := m.SetExtensionTokenHash("hash"); err != nil {
		t.Fatalf("SetExtensionTokenHash: %v", err)
	}

	data, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	var saved map[string]any
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if saved["browserId"] != "b-ghosty" {
		t.Fatalf("browserId = %v, want b-ghosty", saved["browserId"])
	}
	if saved["extensionTokenHash"] != "hash" {
		t.Fatalf("extensionTokenHash = %v, want hash", saved["extensionTokenHash"])
	}
	if _, ok := saved["apiToken"]; ok {
		t.Fatal("ghost field apiToken was re-serialized")
	}
	if _, ok := saved["serverUrl"]; ok {
		t.Fatal("ghost field serverUrl was re-serialized")
	}
}

func TestRegeneratesBrowserIDWhenConfigLacksIt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{"extensionTokenHash":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newManagerInDir(t, dir)
	// TS: a missing/invalid browserId throws inside loadConfig, which the
	// catch swallows into a fresh config — and the token hash is dropped
	// with it.
	if !regexp.MustCompile(`^b-[0-9a-f]{8}$`).MatchString(m.BrowserID()) {
		t.Fatalf("browserId = %q, want a fresh b-<8 hex>", m.BrowserID())
	}
	if m.ExtensionTokenHash() != "" {
		t.Fatalf("extensionTokenHash = %q, want empty after config reset", m.ExtensionTokenHash())
	}
}

func TestBufferCommandLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir, WithBufferTimeout(time.Minute))

	// Offline: cannot buffer.
	if m.BufferCommand("{}", func() {}) {
		t.Fatal("buffered while offline")
	}

	m.SetStatus(protocol.StatusIdleWait)
	timedOut := make(chan struct{}, 1)
	if !m.BufferCommand(`{"id":"buffered"}`, func() { timedOut <- struct{}{} }) {
		t.Fatal("did not buffer while idle_wait")
	}
	// Only one command is buffered.
	if m.BufferCommand(`{"id":"second"}`, func() {}) {
		t.Fatal("buffered a second command")
	}

	env, ok := m.GetBufferedCommand()
	if !ok || env != `{"id":"buffered"}` {
		t.Fatalf("GetBufferedCommand = %q, %v", env, ok)
	}
	if _, ok := m.GetBufferedCommand(); ok {
		t.Fatal("buffer was not consumed")
	}
	select {
	case <-timedOut:
		t.Fatal("timeout fired after the buffer was consumed")
	default:
	}
}

func TestBufferTimeoutFiresAndClears(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir, WithBufferTimeout(20*time.Millisecond))
	m.SetStatus(protocol.StatusIdleWait)

	timedOut := make(chan struct{}, 1)
	if !m.BufferCommand("{}", func() { timedOut <- struct{}{} }) {
		t.Fatal("did not buffer")
	}
	select {
	case <-timedOut:
	case <-time.After(5 * time.Second):
		t.Fatal("buffer timeout never fired")
	}
	if _, ok := m.GetBufferedCommand(); ok {
		t.Fatal("buffer not cleared after timeout")
	}
}

func TestLeavingIdleWaitCancelsBufferTimeout(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir, WithBufferTimeout(20*time.Millisecond))
	m.SetStatus(protocol.StatusIdleWait)

	timedOut := make(chan struct{}, 1)
	if !m.BufferCommand("{}", func() { timedOut <- struct{}{} }) {
		t.Fatal("did not buffer")
	}
	m.SetStatus(protocol.StatusOnline)

	if _, ok := m.GetBufferedCommand(); ok {
		t.Fatal("buffer survived the status flip")
	}
	select {
	case <-timedOut:
		t.Fatal("timeout fired after the status flip")
	case <-time.After(200 * time.Millisecond):
		// The timer was stopped: no callback within several buffer timeouts.
	}
}

func TestCanAcceptCommand(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir)
	if m.CanAcceptCommand() {
		t.Fatal("offline should not accept commands")
	}
	for _, status := range []protocol.BrowserStatus{protocol.StatusOnline, protocol.StatusIdleWait} {
		m.SetStatus(status)
		if !m.CanAcceptCommand() {
			t.Fatalf("status %s should accept commands", status)
		}
	}
	m.SetStatus(protocol.StatusOffline)
	if m.CanAcceptCommand() {
		t.Fatal("offline should not accept commands")
	}
}
