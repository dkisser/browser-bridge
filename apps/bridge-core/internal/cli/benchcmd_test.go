package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"browser-bridge/internal/core"
	"browser-bridge/internal/memory"
)

// The bench CLI has one job beyond producing numbers: it must not be able to
// damage the store it is measuring. Its own store is a throwaway directory, so
// a bug here writes to a temp dir rather than to the cards the daemon has
// learned from real browsing. That property is asserted rather than assumed.

func withBenchHome(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("BB_HOME", dir)
	return dir
}

func TestBenchRunIsSelfContained(t *testing.T) {
	home := withBenchHome(t)

	stdout, stderr, err := runCLI(t, "memory", "bench", "run", "--train", "2", "--stale", "1", "--rebuild", "2")
	if err != nil {
		t.Fatalf("%v\n%s", err, stderr)
	}
	for _, want := range []string{
		"card saves", "By phase", "trained on v1", "v1 card, site redesigned", "card rebuilt on v2",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("the report does not mention %q:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "card COSTS") {
		t.Errorf("the card came out worse than no card somewhere, which the design says is impossible:\n%s", stdout)
	}

	// The run must have written only its log under the data dir. If it had
	// written a card, the fixture's host would be sitting in the real store and
	// would be injected into a real agent's next visit to a made-up domain.
	entries, err := os.ReadDir(filepath.Join(home, "data"))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() {
			t.Errorf("the bench created %s under the data dir; it is meant to work in a throwaway store", e.Name())
		}
		if e.Name() != benchLogName {
			t.Errorf("the bench created %s under the data dir; only the log belongs there", e.Name())
		}
	}
	if _, err := os.Stat(filepath.Join(home, "data", benchLogName)); err != nil {
		t.Errorf("the bench log was not written: %v", err)
	}
}

func TestBenchRunAppendsAcrossInvocations(t *testing.T) {
	withBenchHome(t)
	first, _, err := runCLI(t, "memory", "bench", "run", "--train", "2", "--stale", "0", "--rebuild", "0")
	if err != nil {
		t.Fatal(err)
	}
	second, _, err := runCLI(t, "memory", "bench", "run", "--train", "2", "--stale", "0", "--rebuild", "0")
	if err != nil {
		t.Fatal(err)
	}
	// Same repetitions must give the same numbers, or a trend drawn across two
	// invocations is comparing two different experiments.
	if !strings.Contains(first, "card saves") || !strings.Contains(second, "card saves") {
		t.Errorf("a single-phase run reported no saving:\n%s\n---\n%s", first, second)
	}

	env, err := EnvFromOSEnv()
	if err != nil {
		t.Fatal(err)
	}
	recs, err := readJSONL(filepath.Join(env.DataDir(), benchLogName))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 32 { // 2 reps x 4 tasks x 2 arms, twice
		t.Errorf("the log holds %d records, want 32; it is not accumulating across runs", len(recs))
	}
	for _, r := range recs {
		if r["mode"] != "fixture" {
			t.Errorf("record has mode %v, want fixture", r["mode"])
		}
	}
}

func TestBenchRunRejectsEmptySchedule(t *testing.T) {
	withBenchHome(t)
	_, _, err := runCLI(t, "memory", "bench", "run", "--train", "0", "--stale", "0", "--rebuild", "0")
	if err == nil {
		t.Error("a run with no repetitions was accepted")
	}
}

func TestBenchReportOnAnEmptyLog(t *testing.T) {
	withBenchHome(t)
	stdout, _, err := runCLI(t, "memory", "bench", "report")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "Nothing recorded yet") {
		t.Errorf("an empty log did not say so:\n%s", stdout)
	}
	if !strings.Contains(stdout, "bench record") {
		t.Errorf("an empty log did not say what to do next:\n%s", stdout)
	}
}

func TestBenchRecordDerivesCountsFromTheTrace(t *testing.T) {
	// The point of the live half: the call count comes out of the recorded
	// trace, not out of a human counting. So the test plants a trace and checks
	// the numbers that come back.
	withBenchHome(t)
	seedTrace(t, 5, 2)
	seedCardShown(t, 2, 3)

	stdout, _, err := runCLI(t, "memory", "bench", "record",
		"--host", "news.example.com", "--task", "find the top story",
		"--ok", "--card", "used", "--since", "1h", "--calls", "6", "--failures", "2")
	if err != nil {
		t.Fatal(err)
	}
	// Six, not five: the navigate is a call too, and counting it is the point of
	// reading the trace instead of counting by hand.
	if !strings.Contains(stdout, "6 call(s), 2 rejected") {
		t.Errorf("the trace counts were not read back:\n%s", stdout)
	}
	if strings.Contains(stdout, "!") {
		t.Errorf("a self-consistent report was flagged as a disagreement:\n%s", stdout)
	}

	env, err := EnvFromOSEnv()
	if err != nil {
		t.Fatal(err)
	}
	recs, err := readJSONL(filepath.Join(env.DataDir(), benchLogName))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	rec := recs[0]
	if rec["mode"] != "live" {
		t.Errorf("mode is %v, want live", rec["mode"])
	}
	if got := rec["calls"]; got != float64(6) {
		t.Errorf("calls is %v, want 6", got)
	}
	if got := rec["cardsShown"]; got != float64(2) {
		t.Errorf("cardsShown is %v, want 2; the number of cards offered has to come from the trace, not from the agent", got)
	}
	if got := rec["cardLines"]; got != float64(6) {
		t.Errorf("cardLines is %v, want 6", got)
	}
	if got := rec["failures"]; got != float64(2) {
		t.Errorf("failures is %v, want 2", got)
	}
	if rec["cardUsed"] != true {
		t.Errorf("cardUsed is %v, want true", rec["cardUsed"])
	}
	if rec["crossCheck"] != true {
		t.Errorf("crossCheck is %v, want true", rec["crossCheck"])
	}
}

func TestBenchRecordFlagsADisagreement(t *testing.T) {
	// A window that covers the wrong stretch of session is the most common way
	// a hand-rolled baseline becomes quietly wrong, so the mismatch has to be
	// recorded on the row and not just printed.
	withBenchHome(t)
	seedTrace(t, 5, 2)
	seedCardShown(t, 1, 2)

	stdout, _, err := runCLI(t, "memory", "bench", "record",
		"--host", "news.example.com", "--task", "find the top story",
		"--ok", "--card", "used", "--since", "1h", "--calls", "9")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "you reported 9 calls, the trace shows 6") {
		t.Errorf("the disagreement was not reported:\n%s", stdout)
	}

	env, err := EnvFromOSEnv()
	if err != nil {
		t.Fatal(err)
	}
	recs, err := readJSONL(filepath.Join(env.DataDir(), benchLogName))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 || recs[0]["crossCheck"] != false {
		t.Errorf("the disagreement was not recorded on the row: %+v", recs)
	}
	if _, ok := recs[0]["mismatches"]; !ok {
		t.Error("the record does not carry the mismatches")
	}
}

func TestBenchRecordRejectsNonsense(t *testing.T) {
	withBenchHome(t)
	cases := [][]string{
		{"memory", "bench", "record", "--ok"},                           // no task
		{"memory", "bench", "record", "--task", "t", "--card", "maybe"}, // bad card value
		{"memory", "bench", "record", "--task", "t", "--since", "0s"},   // empty window
	}
	for _, args := range cases {
		if _, _, err := runCLI(t, args...); err == nil {
			t.Errorf("%v was accepted", args)
		}
	}
}

func TestBenchReportSeparatesArms(t *testing.T) {
	withBenchHome(t)
	if _, _, err := runCLI(t, "memory", "bench", "run", "--train", "2", "--stale", "0", "--rebuild", "0"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err := runCLI(t, "memory", "bench", "report", "--mode", "live")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "No records matched") {
		t.Errorf("--mode live should report that nothing matched, since only fixture runs exist:\n%s", stdout)
	}
	stdout, _, err = runCLI(t, "memory", "bench", "report", "--mode", "fixture")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "card offered") {
		t.Errorf("the report does not say how often a card was offered, which is the number that distinguishes 'not helping' from 'not injected':\n%s", stdout)
	}
	if !strings.Contains(stdout, "no card") || !strings.Contains(stdout, "card  ") {
		t.Errorf("the report does not separate the two arms:\n%s", stdout)
	}
}

func TestBenchJSONLRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "log.jsonl")
	for i := 0; i < 3; i++ {
		if err := appendJSONL(path, map[string]any{"i": i}); err != nil {
			t.Fatal(err)
		}
	}
	recs, err := readJSONL(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 3 {
		t.Fatalf("read %d records, want 3", len(recs))
	}
	if recs[2]["i"] != float64(2) {
		t.Errorf("the last record is %v, want i=2", recs[2]["i"])
	}
	// A missing log is not an error; a first run has nothing to report.
	recs, err = readJSONL(filepath.Join(t.TempDir(), "absent.jsonl"))
	if err != nil || len(recs) != 0 {
		t.Errorf("a missing log gave %v / %v, want no error and no records", recs, err)
	}
}

// seedTrace writes okCalls successful and errCalls rejected calls for one host,
// through the real Manager, so the record path reads the same stream the daemon
// would have written.
// seedCardShown plants injection records, which is what makes "was a card
// offered" a derived fact rather than something the report has to take on
// trust.
func seedCardShown(t *testing.T, times, entries int) {
	t.Helper()
	env, err := EnvFromOSEnv()
	if err != nil {
		t.Fatal(err)
	}
	m, err := memory.New(memory.Options{DataDir: env.DataDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()
	// Written straight into the stream rather than through a Manager hook.
	// KindCardShown is part of the stream's closed record set, so a consumer
	// writing one is not reaching into a private — it is using the same
	// contract the daemon writes it through.
	host := "news.example.com"
	for i := 0; i < times; i++ {
		if err := m.Stream().Append(memory.TraceRecord{
			Kind:    memory.KindCardShown,
			AtMs:    time.Now().UnixMilli(),
			Host:    host,
			TabID:   1,
			Browser: "bench",
			Entries: entries,
		}); err != nil {
			t.Fatal(err)
		}
	}
}

func seedTrace(t *testing.T, okCalls, errCalls int) {
	t.Helper()
	env, err := EnvFromOSEnv()
	if err != nil {
		t.Fatal(err)
	}
	m, err := memory.New(memory.Options{DataDir: env.DataDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = m.Close() }()

	host := "news.example.com"
	m.RecordCommand("env-0", "navigate", host, 1, map[string]any{"url": "https://" + host + "/"})
	m.RecordResult("env-0", "navigate", host, 1, core.ResponsePayload{Status: "ok", Data: json.RawMessage(`{"url":"https://` + host + `/"}`)})
	n := 1
	for i := 0; i < okCalls; i++ {
		envID := fmt.Sprintf("env-ok-%d", n)
		n++
		m.RecordCommand(envID, "gettext", host, 1, map[string]any{"selector": "@e1"})
		m.RecordResult(envID, "gettext", host, 1, core.ResponsePayload{
			Status: "ok", Data: json.RawMessage(`{"text":"headline"}`)})
	}
	for i := 0; i < errCalls; i++ {
		envID := fmt.Sprintf("env-err-%d", n)
		n++
		m.RecordCommand(envID, "gettext", host, 1, map[string]any{"selector": "div.nope"})
		m.RecordResult(envID, "gettext", host, 1, core.ResponsePayload{
			Status: "error", Error: "element_not_found", Message: "no match"})
	}
}

// The two halves of this command are two different experiments. The first
// version of the report grouped on (task, arm) and averaged a fixture run into a
// live one, producing a number that described neither — a script on a synthetic
// page and an agent on a real site are not two samples of the same thing.
func TestBenchReportNeverAveragesAcrossModes(t *testing.T) {
	withBenchHome(t)
	if _, _, err := runCLI(t, "memory", "bench", "run", "--train", "2", "--stale", "0", "--rebuild", "0"); err != nil {
		t.Fatal(err)
	}
	// A live record for one of the same task names, with a cost the fixture run
	// would never produce.
	appendLive(t, "read the subject of a named message", true, 17)

	stdout, _, err := runCLI(t, "memory", "bench", "report")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(stdout, "\n")
	var fixtureRow, liveRow string
	for _, l := range lines {
		if !strings.Contains(l, "read the subject of a named message") {
			continue
		}
		switch {
		case strings.HasPrefix(l, "fixture"):
			fixtureRow = l
		case strings.HasPrefix(l, "live"):
			liveRow = l
		}
	}
	if fixtureRow == "" || liveRow == "" {
		t.Fatalf("the two modes did not get their own rows:\n%s", stdout)
	}
	if strings.Contains(fixtureRow, "17.00") || strings.Contains(liveRow, "3.00") {
		t.Errorf("a fixture result and a live result were averaged together:\nfixture: %s\nlive:     %s",
			fixtureRow, liveRow)
	}
	if !strings.Contains(liveRow, "17.00") {
		t.Errorf("the live row lost its own number: %s", liveRow)
	}
}

// A live record whose window caught no calls is not a measurement of the task —
// it is a measurement of the wrong stretch of session. It must not drag a real
// mean toward zero.
func TestBenchReportExcludesEmptyWindows(t *testing.T) {
	withBenchHome(t)
	appendLive(t, "find the top story", true, 9)
	appendLive(t, "find the top story", true, 0) // window missed the task

	stdout, _, err := runCLI(t, "memory", "bench", "report", "--mode", "live")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(stdout, "mean calls   9.00") {
		t.Errorf("the empty window was averaged in:\n%s", stdout)
	}
	if !strings.Contains(stdout, "left out") {
		t.Errorf("the excluded record was not accounted for:\n%s", stdout)
	}
	if !strings.Contains(stdout, "1 live record(s) left out") {
		t.Errorf("the excluded record was not counted:\n%s", stdout)
	}
}

func appendLive(t *testing.T, task string, cardUsed bool, calls int) {
	t.Helper()
	env, err := EnvFromOSEnv()
	if err != nil {
		t.Fatal(err)
	}
	rec := map[string]any{
		"at": time.Now().UnixMilli(), "mode": "live", "task": task,
		"ok": true, "memory": cardUsed, "cardSeen": cardUsed, "cardUsed": cardUsed,
		"calls": calls, "failures": 0, "cost": calls, "crossCheck": calls > 0,
	}
	if calls == 0 {
		rec["mismatches"] = []any{"you said the card was used but no card was injected in this window"}
	}
	if err := appendJSONL(filepath.Join(env.DataDir(), benchLogName), rec); err != nil {
		t.Fatal(err)
	}
}
