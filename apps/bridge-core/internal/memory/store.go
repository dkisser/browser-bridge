package memory

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Store is the per-host card store (ADR-0021): one JSON file per host under
// $BB_HOME/data/cards/, written with the same write-then-rename pattern
// data/config.json already uses (internal/core/state.go saveLocked), because a
// card is read on the injection path and must never be observed half-written.
//
// The file-per-host choice is the ADR's point, not an accident of
// convenience: cards are the one thing a human is expected to read, diff and
// hand-edit when a card is wrong. A single binary blob would make the
// review-and-rollback story in ADR-0019 a UI project instead of `cat` and `git
// diff`.
type Store struct {
	mu    sync.RWMutex
	dir   string
	cards map[string]*SiteCard
}

const cardsDirName = "cards"

// OpenStore prepares the card directory. A store that cannot create its own
// directory is unusable, so this returns an error rather than degrading — the
// caller decides whether memory is optional at the call site, not here.
func OpenStore(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, cardsDirName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("memory: create cards dir: %w", err)
	}
	return &Store{dir: dir, cards: make(map[string]*SiteCard)}, nil
}

// Dir is the cards directory, for the CLI to print.
func (s *Store) Dir() string { return s.dir }

func (s *Store) hostFile(host string) string {
	return filepath.Join(s.dir, safeFileName(host)+".json")
}

// safeFileName maps a host to a single filesystem-safe basename. Hosts come
// from url.Hostname(), so they cannot contain a separator, but they can contain
// characters that are awkward in a filename (a colon-free IPv6 literal can
// still carry brackets) and the store is also reachable from the CLI where a
// hand-typed name is possible — so anything outside a conservative set is
// percent-escaped rather than trusted.
func safeFileName(host string) string {
	var b strings.Builder
	for _, r := range host {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '.', r == '-':
			b.WriteRune(r)
		case r == ':':
			b.WriteString("%3A")
		default:
			fmt.Fprintf(&b, "%%%02X", r)
		}
	}
	name := b.String()
	if name == "" {
		return "_"
	}
	// Refuse to write outside the cards dir, whatever reached us.
	if strings.ContainsAny(name, "/\\") || name == "." || name == ".." {
		return "_"
	}
	return name
}

// Get returns a copy of the card for host. A missing card is (nil, false), not
// an error: having no card for a host is the normal state of the world.
func (s *Store) Get(host string) (*SiteCard, bool) {
	s.mu.RLock()
	c, ok := s.cards[host]
	s.mu.RUnlock()
	if ok {
		return cloneCard(c), true
	}
	c, err := s.load(host)
	if err != nil {
		return nil, false
	}
	return c, c != nil
}

func (s *Store) load(host string) (*SiteCard, error) {
	data, err := os.ReadFile(s.hostFile(host))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var card SiteCard
	if err := json.Unmarshal(data, &card); err != nil {
		// A hand-edited or truncated card must not take the store down: the
		// learner will rebuild it from the stream, which is the truth.
		return nil, fmt.Errorf("memory: parse card %s: %w", host, err)
	}
	if card.Host == "" {
		card.Host = host
	}
	return &card, nil
}

// Put writes the card atomically and caches it. revision is the caller's new
// revision number; the store does not invent one, because the revision is the
// thing the stream's card_revision record has to agree with.
func (s *Store) Put(card *SiteCard) error {
	if card == nil || card.Host == "" {
		return fmt.Errorf("memory: put card: missing host")
	}
	data, err := json.MarshalIndent(card, "", "  ")
	if err != nil {
		return fmt.Errorf("memory: marshal card %s: %w", card.Host, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("memory: create cards dir: %w", err)
	}
	file := s.hostFile(card.Host)
	tmp := file + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("memory: write card: %w", err)
	}
	if err := os.Rename(tmp, file); err != nil {
		return fmt.Errorf("memory: replace card: %w", err)
	}
	// WriteFile's mode only applies on create, so tighten an existing file the
	// same way the config store does.
	if err := os.Chmod(file, 0o600); err != nil {
		return fmt.Errorf("memory: tighten card permissions: %w", err)
	}
	s.cards[card.Host] = cloneCard(card)
	return nil
}

// Remove deletes a card. Used by `bridge memory rm` and by the rollback path.
func (s *Store) Remove(host string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.cards, host)
	if err := os.Remove(s.hostFile(host)); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("memory: remove card: %w", err)
	}
	return nil
}

// Hosts lists every host with a card, sorted.
func (s *Store) Hosts() []string {
	s.mu.RLock()
	cached := make([]string, 0, len(s.cards))
	for h := range s.cards {
		cached = append(cached, h)
	}
	dir := s.dir
	s.mu.RUnlock()

	seen := make(map[string]bool, len(cached))
	for _, h := range cached {
		seen[h] = true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		sort.Strings(cached)
		return cached
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".json") {
			continue
		}
		h := strings.TrimSuffix(name, ".json")
		if c, err := s.load(h); err == nil && c != nil {
			if !seen[c.Host] {
				cached = append(cached, c.Host)
				seen[c.Host] = true
			}
		}
	}
	sort.Strings(cached)
	return cached
}

// Revisions returns the card_revision records for a host, newest first, read
// from the stream. This is the review surface: the diff a human reads before
// accepting or reverting an automatic update.
func (s *Stream) Revisions(host string, limit int) ([]CardRevision, error) {
	recs, _, _, err := s.ReadFrom(0)
	if err != nil {
		return nil, err
	}
	var out []CardRevision
	for _, r := range recs {
		if r.Kind != KindCardRevision || r.Revision == nil || r.Revision.Host != host {
			continue
		}
		out = append(out, *r.Revision)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Revision > out[j].Revision })
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

func cloneCard(c *SiteCard) *SiteCard {
	if c == nil {
		return nil
	}
	data, err := json.Marshal(c)
	if err != nil {
		return c
	}
	var out SiteCard
	if err := json.Unmarshal(data, &out); err != nil {
		return c
	}
	return &out
}
