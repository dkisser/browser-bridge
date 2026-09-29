package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"testing"
	"time"
)

func newManagerInDir(t *testing.T, dir string, opts ...StateOption) *StateManager {
	t.Helper()
	t.Setenv("BB_HOME", dir)
	m, err := NewStateManager(opts...)
	if err != nil {
		t.Fatalf("NewStateManager: %v", err)
	}
	return m
}

// configPath is where the StateManager persists inside a test BB_HOME
// (ADR-0017: the data dir, not the BB_HOME root).
func configPath(dir string) string { return filepath.Join(dir, "data", "config.json") }

// writeExistingConfig plants a config at the current (data dir) location.
func writeExistingConfig(t *testing.T, dir, content string) string {
	t.Helper()
	file := configPath(dir)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
}

func TestWritesConfigUnderDataDir(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir)

	if !regexp.MustCompile(`^b-[0-9a-f]{8}$`).MatchString(m.BrowserID()) {
		t.Fatalf("browserId %q does not match b-<8 hex>", m.BrowserID())
	}
	if _, err := os.Stat(configPath(dir)); err != nil {
		t.Fatalf("config.json not written under data/: %v", err)
	}
}

func TestConfigFilePermissionsAre0600(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir)
	if err := m.SetExtensionTokenHash("hash"); err != nil {
		t.Fatalf("SetExtensionTokenHash: %v", err)
	}
	info, err := os.Stat(configPath(dir))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config.json perm = %o, want 600", perm)
	}
}

func TestLoadTightensPermissionsOnExistingConfig(t *testing.T) {
	dir := t.TempDir()
	file := configPath(dir)
	if err := os.MkdirAll(filepath.Dir(file), 0o700); err != nil {
		t.Fatal(err)
	}
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
	writeExistingConfig(t, dir, `{"browserId":"b-keepme"}`)
	m := newManagerInDir(t, dir)
	if m.BrowserID() != "b-keepme" {
		t.Fatalf("browserId = %q, want b-keepme", m.BrowserID())
	}
}

func TestDoesNotReserializeGhostFields(t *testing.T) {
	dir := t.TempDir()
	ghost := `{"browserId":"b-ghosty","apiToken":"ghost-token","serverUrl":"ws://ghost"}`
	file := writeExistingConfig(t, dir, ghost)

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
	writeExistingConfig(t, dir, `{"extensionTokenHash":"x"}`)
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

// A pre-ADR-0017 install keeps config.json at the BB_HOME root; the first
// daemon start after the upgrade moves it into data/ so the extension stays
// paired.
func TestMigratesLegacyRootConfig(t *testing.T) {
	dir := t.TempDir()
	legacy := filepath.Join(dir, "config.json")
	if err := os.WriteFile(legacy, []byte(`{"browserId":"b-legacy","extensionTokenHash":"h"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newManagerInDir(t, dir)
	if m.BrowserID() != "b-legacy" {
		t.Fatalf("browserId = %q, want b-legacy", m.BrowserID())
	}
	if m.ExtensionTokenHash() != "h" {
		t.Fatalf("extensionTokenHash = %q, want h", m.ExtensionTokenHash())
	}
	if _, err := os.Stat(legacy); !os.IsNotExist(err) {
		t.Fatalf("legacy config.json still at BB_HOME root: %v", err)
	}
	if _, err := os.Stat(configPath(dir)); err != nil {
		t.Fatalf("config.json not migrated into data/: %v", err)
	}
}

// When both locations hold a config the data dir wins; the legacy file is
// left in place (an old binary still finds it after a downgrade).
func TestDataDirConfigWinsOverLegacyRootConfig(t *testing.T) {
	dir := t.TempDir()
	writeExistingConfig(t, dir, `{"browserId":"b-data"}`)
	legacy := filepath.Join(dir, "config.json")
	if err := os.WriteFile(legacy, []byte(`{"browserId":"b-root"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newManagerInDir(t, dir)
	if m.BrowserID() != "b-data" {
		t.Fatalf("browserId = %q, want b-data", m.BrowserID())
	}
	if _, err := os.Stat(legacy); err != nil {
		t.Fatalf("legacy config.json should be left alone: %v", err)
	}
}

func TestBufferCommandLifecycle(t *testing.T) {
	dir := t.TempDir()
	m := newManagerInDir(t, dir, WithBufferTimeout(time.Minute))

	// Offline: cannot buffer.
	if m.BufferCommand("{}", func() {}) {
		t.Fatal("buffered while offline")
	}

	m.SetStatus(StatusIdleWait)
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
	m.SetStatus(StatusIdleWait)

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
	m.SetStatus(StatusIdleWait)

	timedOut := make(chan struct{}, 1)
	if !m.BufferCommand("{}", func() { timedOut <- struct{}{} }) {
		t.Fatal("did not buffer")
	}
	m.SetStatus(StatusOnline)

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
	for _, status := range []BrowserStatus{StatusOnline, StatusIdleWait} {
		m.SetStatus(status)
		if !m.CanAcceptCommand() {
			t.Fatalf("status %s should accept commands", status)
		}
	}
	m.SetStatus(StatusOffline)
	if m.CanAcceptCommand() {
		t.Fatal("offline should not accept commands")
	}
}
