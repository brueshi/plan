package rexctl

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
)

// fakeRex answers block ls and program_status from fixed data and
// records every other call.
type fakeRex struct {
	blocks   string            // JSON for block ls
	statuses map[string]string // block ID to program_status JSON
	calls    [][]string
}

func (f *fakeRex) run(_ context.Context, args ...string) ([]byte, error) {
	switch {
	case args[0] == "block" && args[1] == "ls":
		return []byte(f.blocks), nil
	case args[0] == "block" && args[1] == "call":
		block := args[slices.Index(args, "-b")+1]
		if s, ok := f.statuses[block]; ok {
			return []byte(s), nil
		}
		return nil, errors.New("no such block")
	}
	f.calls = append(f.calls, args)
	return nil, nil
}

func blocks(pairs ...string) string {
	var parts []string
	for i := 0; i < len(pairs); i += 2 {
		parts = append(parts, fmt.Sprintf(`{"block_id":%q,"window_id":%q}`, pairs[i], pairs[i+1]))
	}
	return `{"blocks":[` + strings.Join(parts, ",") + `]}`
}

func status(app, state, at string) string {
	return fmt.Sprintf(`{"records":[{"state":%q,"effective_app":%q,"updated_at":%q}]}`, state, app, at)
}

func TestFindAgentPrefersSameTab(t *testing.T) {
	f := &fakeRex{
		blocks: blocks("self", "w1", "shell", "w1", "near", "w1", "far", "w2"),
		statuses: map[string]string{
			"self":  status("plan", "idle", "2026-10-28T10:00:00Z"),
			"shell": `{"records":[]}`,
			"near":  status(AgentApp, "idle", "2026-10-28T09:00:00Z"),
			"far":   status(AgentApp, "working", "2026-10-28T11:00:00Z"),
		},
	}
	a, err := FindAgent(context.Background(), f.run, "s", "self")
	if err != nil {
		t.Fatal(err)
	}
	if a.Block != "near" {
		t.Fatalf("picked %q, want the agent in the same tab", a.Block)
	}
}

func TestFindAgentFallsBackToLatestInSession(t *testing.T) {
	f := &fakeRex{
		blocks: blocks("self", "w1", "old", "w2", "new", "w3"),
		statuses: map[string]string{
			"old": status(AgentApp, "idle", "2026-10-28T09:00:00Z"),
			"new": status(AgentApp, "done", "2026-10-28T11:00:00Z"),
		},
	}
	a, err := FindAgent(context.Background(), f.run, "s", "self")
	if err != nil {
		t.Fatal(err)
	}
	if a.Block != "new" {
		t.Fatalf("picked %q, want the latest to report", a.Block)
	}
}

func TestFindAgentNone(t *testing.T) {
	f := &fakeRex{blocks: blocks("self", "w1", "shell", "w1")}
	if _, err := FindAgent(context.Background(), f.run, "s", "self"); !errors.Is(err, ErrNoAgent) {
		t.Fatalf("err = %v, want ErrNoAgent", err)
	}
}

func TestSendTypesThenPressesEnter(t *testing.T) {
	f := &fakeRex{}
	err := Send(context.Background(), f.run, "s", Agent{Block: "b1", State: "idle"}, "- leading dash task")
	if err != nil {
		t.Fatal(err)
	}
	want := [][]string{
		{"send", "-s", "s", "-b", "b1", "--", "- leading dash task"},
		{"send-key", "-s", "s", "-b", "b1", "enter"},
	}
	if fmt.Sprint(f.calls) != fmt.Sprint(want) {
		t.Fatalf("calls %q, want %q", f.calls, want)
	}
}

func TestSendRefusesBlockedAgentAndNewlines(t *testing.T) {
	f := &fakeRex{}
	if err := Send(context.Background(), f.run, "s", Agent{Block: "b1", State: "blocked"}, "task"); !errors.Is(err, ErrAgentBlocked) {
		t.Fatalf("err = %v, want ErrAgentBlocked", err)
	}
	if err := Send(context.Background(), f.run, "s", Agent{Block: "b1"}, "two\nlines"); err == nil {
		t.Fatal("multi-line message accepted")
	}
	if len(f.calls) != 0 {
		t.Fatalf("nothing should be typed, got %q", f.calls)
	}
}
