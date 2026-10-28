package view

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/brueshi/plan/internal/active"
)

// following returns a view that follows *target, where nil means the
// session has no plan, plus two plan files to point it at.
func following(t *testing.T, target **active.Target) (m Model, first, second string) {
	t.Helper()
	isolate(t)
	dir := t.TempDir()
	first, second = filepath.Join(dir, "first.md"), filepath.Join(dir, "second.md")
	for path, content := range map[string]string{
		first:  "- [ ] One\n- [ ] Two\n",
		second: "# Second\n\n## Stages\n- [x] Alpha\n- [ ] Beta\n",
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	m = Follow(func() (active.Target, error) {
		if *target == nil {
			return active.Target{}, active.ErrNoPlan
		}
		return **target, nil
	})
	return update(t, m, tea.WindowSizeMsg{Width: 80, Height: 20}), first, second
}

// pollOnce runs one poll the way the tick would.
func pollOnce(t *testing.T, m Model) Model {
	t.Helper()
	if msg, ok := m.checkFile()().(loadedMsg); ok {
		return update(t, m, msg)
	}
	return m
}

func TestFollowWaitsThenSwitches(t *testing.T) {
	var target *active.Target
	m, first, second := following(t, &target)

	if out := screen(m); !strings.Contains(out, "No plan for this session yet") || strings.Count(out, "\n")+1 != 20 {
		t.Fatalf("waiting screen:\n%s", out)
	}
	if ps := m.View().ProgramStatus; ps.Message != "Waiting for a plan" {
		t.Fatalf("status %+v", ps)
	}
	if m = press(t, m, "a"); m.mode != normal {
		t.Fatal("editing keys must do nothing without a plan")
	}

	target = &active.Target{Path: first}
	m = press(t, pollOnce(t, m), "j")
	if !strings.Contains(screen(m), "first.md") || cursorText(m) != "Two" {
		t.Fatalf("after the plan appeared:\n%s", screen(m))
	}

	target = &active.Target{Path: second, Section: "Stages"}
	m = pollOnce(t, m)
	if !strings.Contains(screen(m), "second.md › Stages") || cursorText(m) != "Alpha" {
		t.Fatalf("switch should land on the new plan's first task:\n%s", screen(m))
	}

	target = nil
	if m = pollOnce(t, m); !strings.Contains(screen(m), "No plan for this session yet") {
		t.Fatalf("back to waiting:\n%s", screen(m))
	}
}

func TestFollowStillPicksUpEdits(t *testing.T) {
	var target *active.Target
	m, first, _ := following(t, &target)
	target = &active.Target{Path: first}
	m = pollOnce(t, m)

	if err := os.WriteFile(first, []byte("- [x] One\n- [ ] Two\n- [ ] Three\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if m = pollOnce(t, m); len(m.items) != 3 || !strings.Contains(screen(m), "1/3") {
		t.Fatalf("edit not picked up:\n%s", screen(m))
	}
}

func TestSwitchCancelsHalfTypedEdit(t *testing.T) {
	var target *active.Target
	m, first, second := following(t, &target)
	target = &active.Target{Path: first}
	m = pollOnce(t, m)

	m = typeText(t, press(t, m, "a"), "half typed")
	target = &active.Target{Path: second}
	m = pollOnce(t, m)
	if m.mode != normal {
		t.Fatalf("mode %v: a switch must cancel the edit", m.mode)
	}
	m = press(t, m, "enter")
	if b, _ := os.ReadFile(second); strings.Contains(string(b), "half typed") {
		t.Fatal("text typed for the old plan landed in the new one")
	}
}

func TestWaitingAsksAgentForItsPlan(t *testing.T) {
	var target *active.Target
	m, _, _ := following(t, &target)
	var typed [][]string
	m.rexSession, m.rexBlock, m.runRex = "s", "self", fakeAgent("idle", &typed)

	m = press(t, m, "w")
	if len(typed) != 2 || !strings.Contains(typed[0][len(typed[0])-1], "plan new") {
		t.Fatalf("typed %q, want the write-your-plan prompt", typed)
	}
	if !strings.Contains(screen(m), "Asked claude to write its plan") {
		t.Fatalf("no confirmation:\n%s", screen(m))
	}
}

func TestWritePlanPromptIsOneLine(t *testing.T) {
	// A newline would make rex send submit part of the prompt.
	if strings.ContainsAny(writePlanPrompt, "\r\n") {
		t.Fatal("writePlanPrompt must be a single line")
	}
}
