package memory

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"browser-bridge/internal/core"
)

// The router records from every command goroutine at once, so the tab state is
// shared mutable state touched by all of them. These tests exist to run under
// -race: driving the manager sequentially (as the behavioural tests do) cannot
// see a race at all.
func TestConcurrentRecordingIsRaceFree(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")

	const tabs = 8
	const perTab = 25
	var wg sync.WaitGroup
	for tab := 0; tab < tabs; tab++ {
		wg.Add(1)
		go func(tab int) {
			defer wg.Done()
			host := fmt.Sprintf("host-%d.example.com", tab)
			for i := 0; i < perTab; i++ {
				env := fmt.Sprintf("t%d-c%d", tab, i)
				m.RecordCommand(env, "navigate", tab, map[string]any{"url": "https://" + host + "/"})
				m.RecordResult(env, "navigate", tab, core.ResponsePayload{
					Status: "ok",
					Data:   json.RawMessage(`{"url":"https://` + host + `/","title":"T"}`),
				})
				sEnv := fmt.Sprintf("t%d-s%d", tab, i)
				m.RecordCommand(sEnv, "snapshot", tab, nil)
				m.RecordResult(sEnv, "snapshot", tab, okPayload(snapshotJSON(testSnapshot)))
				// Ask for the note on the same tab, concurrently with the
				// other tabs' notes: this is the read side of the same state.
				_ = m.TakeSiteNote("navigate", tab)
				_ = m.TakeSiteNote("snapshot", tab)
			}
		}(tab)
	}
	wg.Wait()
}

// TakeSiteNote and RecordResult racing on the same tab is the exact shape of
// "the agent asked for its card while the next command was landing".
func TestTakeNoteRacesWithLanding(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")

	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		env := fmt.Sprintf("e%d", i)
		wg.Add(2)
		go func() {
			defer wg.Done()
			m.RecordCommand(env, "navigate", 1, map[string]any{"url": "https://news.example.com/"})
			m.RecordResult(env, "navigate", 1, core.ResponsePayload{
				Status: "ok",
				Data:   json.RawMessage(`{"url":"https://news.example.com/"}`),
			})
		}()
		go func() {
			defer wg.Done()
			_ = m.TakeSiteNote("navigate", 1)
		}()
	}
	wg.Wait()
}

// The learner runs on its own goroutine while the router keeps writing, which is
// the arrangement the background loop actually has.
func TestLearnerRunsWhileRecordingContinues(t *testing.T) {
	m := newTestManager(t, "b-1")
	seedCard(t, m, "news.example.com")

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 30; i++ {
			env := fmt.Sprintf("e%d", i)
			m.RecordCommand(env, "navigate", i%3, map[string]any{"url": "https://news.example.com/"})
			m.RecordResult(env, "navigate", i%3, core.ResponsePayload{
				Status: "ok", Data: json.RawMessage(`{"url":"https://news.example.com/"}`),
			})
		}
	}()
	for i := 0; i < 10; i++ {
		if err := m.LearnNow(); err != nil {
			t.Fatalf("LearnNow: %v", err)
		}
	}
	<-done
	if err := m.LearnNow(); err != nil {
		t.Fatalf("LearnNow: %v", err)
	}
}
