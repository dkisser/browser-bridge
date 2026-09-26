package core

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// DefaultBufferTimeout is BUFFER_TIMEOUT_MS in state.ts.
const DefaultBufferTimeout = 5 * time.Second

const configFileName = "config.json"

// fileConfig is the on-disk shape. Only these two fields survive a load —
// the TS load destructures exactly them so ghost fields from pre-merge
// configs (apiToken, serverUrl) are never re-serialized on save.
type fileConfig struct {
	BrowserID          string `json:"browserId"`
	ExtensionTokenHash string `json:"extensionTokenHash,omitempty"`
}

type bufferedCommand struct {
	envelope   string
	receivedAt time.Time
	timer      *time.Timer
}

// StateOption customizes a StateManager.
type StateOption func(*StateManager)

// WithBufferTimeout overrides the 5s command-buffer timeout (tests).
func WithBufferTimeout(d time.Duration) StateOption {
	return func(m *StateManager) { m.bufferTimeout = d }
}

// StateManager holds the bridge config and browser status — the Go port of
// src/state.ts from the deleted Bun implementation: the on-disk bridge
// config (browserId + extension token hash) plus the in-memory browser
// status and the 5-second command buffer that rides out extension
// service-worker reconnects. All methods are safe for concurrent use (the TS
// original relied on the single-threaded event loop; the Go servers call in
// from many goroutines).
type StateManager struct {
	mu            sync.Mutex
	dir           string
	config        fileConfig
	status        BrowserStatus
	buffered      *bufferedCommand
	bufferTimeout time.Duration
}

// NewStateManager loads (or initializes) the config under $BB_HOME, falling
// back to ~/.browser-bridge. A load failure (missing file, bad JSON, missing
// browserId) produces a fresh config with a generated browserId, mirroring
// the TS fall-through; a failure to persist that fresh config is fatal, as
// in TS.
func NewStateManager(opts ...StateOption) (*StateManager, error) {
	dir := os.Getenv("BB_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("resolve home directory: %w", err)
		}
		dir = filepath.Join(home, ".browser-bridge")
	}
	m := &StateManager{
		dir:           dir,
		status:        StatusOffline,
		bufferTimeout: DefaultBufferTimeout,
	}
	for _, opt := range opts {
		opt(m)
	}
	if cfg, ok := loadConfig(filepath.Join(dir, configFileName)); ok {
		m.config = cfg
		return m, nil
	}
	m.config = fileConfig{BrowserID: newBrowserID()}
	if err := m.saveLocked(); err != nil {
		return nil, fmt.Errorf("write initial config: %w", err)
	}
	return m, nil
}

func newBrowserID() string {
	// TS: `b-${crypto.randomUUID().slice(0, 8)}` — the first 8 hex chars of a
	// random UUID, i.e. 4 random bytes rendered as hex.
	var b [4]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("crypto/rand unavailable: %v", err))
	}
	return "b-" + hex.EncodeToString(b[:])
}

// loadConfig returns the persisted config, keeping only the fields the TS
// destructure keeps. ok is false on any failure (including the chmod that
// tightens permissions on files written by older versions) — the TS code
// catches all of these identically and falls through to a fresh config.
func loadConfig(file string) (cfg fileConfig, ok bool) {
	data, err := os.ReadFile(file)
	if err != nil {
		return fileConfig{}, false
	}
	if err := os.Chmod(file, 0o600); err != nil {
		return fileConfig{}, false
	}
	var raw struct {
		BrowserID          any `json:"browserId"`
		ExtensionTokenHash any `json:"extensionTokenHash"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fileConfig{}, false
	}
	browserID, ok := raw.BrowserID.(string)
	if !ok {
		return fileConfig{}, false
	}
	cfg.BrowserID = browserID
	if hash, ok := raw.ExtensionTokenHash.(string); ok {
		cfg.ExtensionTokenHash = hash
	}
	return cfg, true
}

func (m *StateManager) saveLocked() error {
	if err := os.MkdirAll(m.dir, 0o700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	data, err := json.MarshalIndent(m.config, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	file := filepath.Join(m.dir, configFileName)
	if err := os.WriteFile(file, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	// Matches TS saveConfigSync: chmod after write, because WriteFile's mode
	// only applies when the file is created, not when it already exists.
	if err := os.Chmod(file, 0o600); err != nil {
		return fmt.Errorf("tighten config permissions: %w", err)
	}
	return nil
}

func (m *StateManager) BrowserID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config.BrowserID
}

func (m *StateManager) ExtensionTokenHash() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.config.ExtensionTokenHash
}

func (m *StateManager) SetExtensionTokenHash(hash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.ExtensionTokenHash = hash
	return m.saveLocked()
}

func (m *StateManager) ClearExtensionTokenHash() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.config.ExtensionTokenHash = ""
	return m.saveLocked()
}

func (m *StateManager) Status() BrowserStatus {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status
}

// SetStatus records the browser status. Leaving idle_wait drops any buffered
// command and cancels its timeout — the router reads the buffer *before*
// flipping status precisely because of this (see router.handleBrowserConnect
// in router.ts).
func (m *StateManager) SetStatus(status BrowserStatus) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.status = status
	if status != StatusIdleWait && m.buffered != nil {
		m.buffered.timer.Stop()
		m.buffered = nil
	}
}

// CanAcceptCommand reports whether the browser may accept a command.
func (m *StateManager) CanAcceptCommand() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.status == StatusOnline || m.status == StatusIdleWait
}

// BufferCommand holds one command while the extension is between reconnects
// (status idle_wait). Only one command is ever buffered; a second one within
// the budget is rejected so the router can answer cannot_buffer. onTimeout
// runs after the buffer expires, with the buffer already cleared.
func (m *StateManager) BufferCommand(envelope string, onTimeout func()) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.status != StatusIdleWait {
		return false
	}
	if m.buffered != nil {
		return false
	}
	b := &bufferedCommand{envelope: envelope, receivedAt: time.Now()}
	b.timer = time.AfterFunc(m.bufferTimeout, func() {
		m.mu.Lock()
		if m.buffered != b {
			m.mu.Unlock()
			return
		}
		m.buffered = nil
		m.mu.Unlock()
		onTimeout()
	})
	m.buffered = b
	return true
}

// GetBufferedCommand consumes the buffered command, canceling its timeout.
func (m *StateManager) GetBufferedCommand() (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.buffered == nil {
		return "", false
	}
	envelope := m.buffered.envelope
	m.buffered.timer.Stop()
	m.buffered = nil
	return envelope, true
}
