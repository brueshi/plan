package view

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

// isolate keeps the lock files of tasklist.Update out of the real cache.
func isolate(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
}

func openPlan(t *testing.T, content, section string) (Model, string) {
	t.Helper()
	isolate(t)
	path := filepath.Join(t.TempDir(), "PLAN.md")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := New(path, section)
	if err != nil {
		t.Fatal(err)
	}
	return update(t, m, tea.WindowSizeMsg{Width: 80, Height: 24}), path
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	}
	r := []rune(k)[0]
	return tea.KeyPressMsg{Code: r, Text: k}
}

// update sends msg and runs the commands it returns, feeding loads
// back in the way the Bubble Tea runtime would.
func update(t *testing.T, m Model, msg tea.Msg) Model {
	t.Helper()
	next, cmd := m.Update(msg)
	m = next.(Model)
	for _, c := range flatten(cmd) {
		switch result := c().(type) {
		case loadedMsg, sentMsg:
			m = update(t, m, result)
		}
	}
	return m
}

func flatten(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	return []tea.Cmd{cmd}
}

func press(t *testing.T, m Model, keys ...string) Model {
	t.Helper()
	for _, k := range keys {
		m = update(t, m, keyMsg(k))
	}
	return m
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		if r == ' ' {
			m = press(t, m, "space")
			continue
		}
		m = press(t, m, string(r))
	}
	return m
}

func fileText(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func screen(m Model) string {
	return ansi.Strip(m.View().Content)
}

func cursorText(m Model) string {
	return m.items[m.cursor].Text
}

func TestToggleStates(t *testing.T) {
	m, path := openPlan(t, "- [ ] A\n- [ ] B\n", "")

	m = press(t, m, "j", "space")
	if got := fileText(t, path); got != "- [ ] A\n- [x] B\n" {
		t.Fatalf("after space: %q", got)
	}
	m = press(t, m, "space")
	if got := fileText(t, path); got != "- [ ] A\n- [ ] B\n" {
		t.Fatalf("second space should clear: %q", got)
	}
	press(t, m, "~")
	if got := fileText(t, path); got != "- [ ] A\n- [~] B\n" {
		t.Fatalf("after ~: %q", got)
	}
}

func TestAddSiblingThenSubtask(t *testing.T) {
	m, path := openPlan(t, "- [ ] A\n  - [ ] A1\n- [ ] B\n", "")

	m = press(t, m, "a")
	m = typeText(t, m, "New task")
	m = press(t, m, "enter")
	if got := fileText(t, path); got != "- [ ] A\n  - [ ] A1\n- [ ] New task\n- [ ] B\n" {
		t.Fatalf("after add: %q", got)
	}
	if cursorText(m) != "New task" {
		t.Fatalf("cursor on %q, want the new item", cursorText(m))
	}

	m = press(t, m, "A")
	m = typeText(t, m, "Sub")
	m = press(t, m, "enter")
	want := "- [ ] A\n  - [ ] A1\n- [ ] New task\n  - [ ] Sub\n- [ ] B\n"
	if got := fileText(t, path); got != want {
		t.Fatalf("after subtask: %q", got)
	}
	if cursorText(m) != "Sub" {
		t.Fatalf("cursor on %q, want Sub", cursorText(m))
	}
}

func TestAddToEmptyPlan(t *testing.T) {
	m, path := openPlan(t, "# Plan\n", "")
	if !strings.Contains(screen(m), "No tasks yet") {
		t.Fatalf("empty state missing:\n%s", screen(m))
	}
	m = press(t, m, "A") // A falls back to a plain add with no items
	m = typeText(t, m, "First")
	press(t, m, "enter")
	if got := fileText(t, path); got != "# Plan\n\n- [ ] First\n" {
		t.Fatalf("got %q", got)
	}
}

func TestEditAndCancel(t *testing.T) {
	m, path := openPlan(t, "- [ ] Draft\n", "")

	m = press(t, m, "e")
	m = typeText(t, m, " v2")
	m = press(t, m, "enter")
	if got := fileText(t, path); got != "- [ ] Draft v2\n" {
		t.Fatalf("after edit: %q", got)
	}

	m = press(t, m, "a")
	m = typeText(t, m, "abandoned")
	m = press(t, m, "esc")
	if got := fileText(t, path); got != "- [ ] Draft v2\n" {
		t.Fatalf("esc should not write: %q", got)
	}
	if m.mode != normal {
		t.Fatalf("mode %v after esc, want normal", m.mode)
	}
}

func TestDelete(t *testing.T) {
	m, path := openPlan(t, "- [ ] A\n  - [ ] A1\n- [ ] B\n", "")

	m = press(t, m, "G", "d", "d")
	if got := fileText(t, path); got != "- [ ] A\n  - [ ] A1\n" {
		t.Fatalf("leaf delete: %q", got)
	}

	m = press(t, m, "g", "g", "d", "d")
	if m.mode != confirmingDelete || !strings.Contains(screen(m), "subtasks? y/n") {
		t.Fatalf("deleting a parent should ask first:\n%s", screen(m))
	}
	m = press(t, m, "n")
	if got := fileText(t, path); got != "- [ ] A\n  - [ ] A1\n" {
		t.Fatalf("n should keep the file: %q", got)
	}

	press(t, m, "d", "d", "y")
	if got := fileText(t, path); got != "" {
		t.Fatalf("y should delete the subtree: %q", got)
	}
}

func TestMoveKeepsCursorOnItem(t *testing.T) {
	m, path := openPlan(t, "- [ ] A\n- [ ] B\n- [ ] C\n", "")

	m = press(t, m, "J")
	if got := fileText(t, path); got != "- [ ] B\n- [ ] A\n- [ ] C\n" {
		t.Fatalf("after J: %q", got)
	}
	if cursorText(m) != "A" {
		t.Fatalf("cursor on %q, want A", cursorText(m))
	}

	m = press(t, m, "K", "K")
	if got := fileText(t, path); got != "- [ ] A\n- [ ] B\n- [ ] C\n" {
		t.Fatalf("K at the top should stop quietly: %q", got)
	}
	if m.err != nil {
		t.Fatalf("unexpected error: %v", m.err)
	}
}

func TestOutsideEditReloadsAndKeepsCursor(t *testing.T) {
	m, path := openPlan(t, "- [ ] A\n- [ ] B\n", "")
	m = press(t, m, "j")

	if msg := m.checkFile()(); msg != nil {
		t.Fatalf("unchanged file should not reload, got %#v", msg)
	}

	// An agent adds a task above the cursor and finishes A.
	if err := os.WriteFile(path, []byte("- [ ] Z\n- [x] A\n- [ ] B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	msg := m.checkFile()()
	loaded, ok := msg.(loadedMsg)
	if !ok {
		t.Fatalf("changed file should reload, got %#v", msg)
	}
	m = update(t, m, loaded)

	if len(m.items) != 3 || cursorText(m) != "B" {
		t.Fatalf("items %d, cursor on %q; want 3 items with the cursor still on B", len(m.items), cursorText(m))
	}
	if !strings.Contains(screen(m), "1/3") {
		t.Fatalf("progress not updated:\n%s", screen(m))
	}
}

func TestEditAfterOutsideChangeUsesFreshFile(t *testing.T) {
	m, path := openPlan(t, "- [ ] A\n- [ ] B\n", "")
	m = press(t, m, "j")

	// The file changes before the view's next poll.
	if err := os.WriteFile(path, []byte("- [ ] New\n- [ ] A\n- [ ] B\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	press(t, m, "space")
	if got := fileText(t, path); got != "- [ ] New\n- [ ] A\n- [x] B\n" {
		t.Fatalf("toggle should hit B in the fresh file: %q", got)
	}
}

func TestSectionScope(t *testing.T) {
	src := "# Doc\n\n## Plan\n- [ ] In plan\n\n## Notes\n- [ ] Elsewhere\n"
	m, path := openPlan(t, src, "plan")

	if len(m.items) != 1 || !strings.Contains(screen(m), "PLAN.md › Plan") {
		t.Fatalf("section view wrong:\n%s", screen(m))
	}
	m = press(t, m, "a")
	m = typeText(t, m, "Added")
	press(t, m, "enter")
	want := "# Doc\n\n## Plan\n- [ ] In plan\n- [ ] Added\n\n## Notes\n- [ ] Elsewhere\n"
	if got := fileText(t, path); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestMissingSectionFailsAtStart(t *testing.T) {
	isolate(t)
	path := filepath.Join(t.TempDir(), "PLAN.md")
	if err := os.WriteFile(path, []byte("## Real\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(path, "Imaginary"); err == nil || !strings.Contains(err.Error(), "Real") {
		t.Fatalf("err = %v, want one listing the real headings", err)
	}
}

func TestErrorShowsInFooter(t *testing.T) {
	m, path := openPlan(t, "- [ ] A\n", "")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	m = press(t, m, "space")
	if m.err == nil || !strings.Contains(screen(m), "PLAN.md was moved or deleted") {
		t.Fatalf("error not shown:\n%s", screen(m))
	}
	m = press(t, m, "j")
	if m.err != nil {
		t.Fatal("the next key should clear the error")
	}
}

func TestRender(t *testing.T) {
	long := strings.Repeat("word ", 20)
	m, _ := openPlan(t, "- [x] Done\n- [ ] "+long+"\n", "")
	m = update(t, m, tea.WindowSizeMsg{Width: 40, Height: 20})
	out := screen(m)

	for _, want := range []string{"PLAN.md", "1/2 ▓▓▓▓▓░░░░░", "▸ [x] Done", "space done"} {
		if !strings.Contains(out, want) {
			t.Errorf("screen missing %q:\n%s", want, out)
		}
	}
	for _, line := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("line wider than the window (%d): %q", w, line)
		}
	}
	// Wrapped text hangs under the text column.
	if !strings.Contains(out, "\n      word") {
		t.Errorf("long item did not wrap under its text:\n%s", out)
	}
	if lines := strings.Count(out, "\n") + 1; lines != 20 {
		t.Errorf("rendered %d lines, want the full height of 20", lines)
	}
}

func TestScrollKeepsCursorVisible(t *testing.T) {
	var src strings.Builder
	for i := range 30 {
		fmt.Fprintf(&src, "- [ ] task %02d\n", i)
	}
	m, _ := openPlan(t, src.String(), "")
	m = update(t, m, tea.WindowSizeMsg{Width: 40, Height: 10})

	m = press(t, m, "G")
	if out := screen(m); !strings.Contains(out, "▸ [ ] task 29") || strings.Contains(out, "task 00") {
		t.Fatalf("bottom not shown:\n%s", out)
	}
	m = press(t, m, "k")
	if m.offset != 24 {
		t.Fatalf("offset %d after one step up, want the window to stay at 24", m.offset)
	}
	m = press(t, m, "g", "g")
	if out := screen(m); !strings.Contains(out, "▸ [ ] task 00") {
		t.Fatalf("top not shown:\n%s", out)
	}
}

// fakeAgent stands in for the rex CLI with one claude pane beside
// the view, and records what gets typed into it.
func fakeAgent(state string, typed *[][]string) func(context.Context, ...string) ([]byte, error) {
	return func(_ context.Context, args ...string) ([]byte, error) {
		switch {
		case args[0] == "block" && args[1] == "ls":
			return []byte(`{"blocks":[{"block_id":"self","window_id":"w"},{"block_id":"agent","window_id":"w"}]}`), nil
		case args[0] == "block" && args[1] == "call":
			if slices.Contains(args, "agent") {
				return []byte(`{"records":[{"state":"` + state + `","effective_app":"claude-code","updated_at":"2026-10-28T10:00:00Z"}]}`), nil
			}
			return []byte(`{"records":[]}`), nil
		}
		*typed = append(*typed, args)
		return nil, nil
	}
}

func TestSendToAgent(t *testing.T) {
	m, path := openPlan(t, "## Rollout\n- [ ] A\n- [ ] Ship it\n", "Rollout")
	var typed [][]string
	m.rexSession, m.rexBlock, m.runRex = "s", "self", fakeAgent("idle", &typed)

	m = press(t, m, "j", "s")
	want := "Work on this task from the plan at " + path + "#Rollout: Ship it"
	if len(typed) != 2 || typed[0][len(typed[0])-1] != want || typed[1][len(typed[1])-1] != "enter" {
		t.Fatalf("typed %q, want the message then enter", typed)
	}
	if !strings.Contains(screen(m), "Sent to claude: Ship it") {
		t.Fatalf("no confirmation:\n%s", screen(m))
	}
}

func TestSendRefusesBlockedAgent(t *testing.T) {
	m, _ := openPlan(t, "- [ ] A\n", "")
	var typed [][]string
	m.rexSession, m.rexBlock, m.runRex = "s", "self", fakeAgent("blocked", &typed)

	m = press(t, m, "s")
	if len(typed) != 0 || !strings.Contains(screen(m), "waiting for an answer") {
		t.Fatalf("typed %q into a blocked agent:\n%s", typed, screen(m))
	}
}

func TestSendOutsideRex(t *testing.T) {
	m, _ := openPlan(t, "- [ ] A\n", "")
	m.rexSession = ""
	if m = press(t, m, "s"); !strings.Contains(screen(m), "not running in Rex") {
		t.Fatalf("no error:\n%s", screen(m))
	}
}

func TestProgramStatus(t *testing.T) {
	m, _ := openPlan(t, "- [x] A\n", "")
	if ps := m.View().ProgramStatus; ps.State != tea.ProgramStateIdle {
		t.Fatalf("opening a finished plan reported %q, want idle", ps.State)
	}

	m, _ = openPlan(t, "- [x] A\n- [ ] B\n", "")
	ps := m.View().ProgramStatus
	if ps.State != tea.ProgramStateIdle || ps.Message != "1 of 2 tasks done" || ps.App != "plan" {
		t.Fatalf("in progress: %+v", ps)
	}

	m = press(t, m, "j", "space")
	if ps := m.View().ProgramStatus; ps.State != tea.ProgramStateDone || ps.Message != "All 2 tasks done" {
		t.Fatalf("after the last task: %+v", ps)
	}
	m = press(t, m, "space")
	if ps := m.View().ProgramStatus; ps.State != tea.ProgramStateIdle {
		t.Fatalf("reopening a task: %+v", ps)
	}
}

func TestQuit(t *testing.T) {
	m, _ := openPlan(t, "- [ ] A\n", "")
	_, cmd := m.Update(keyMsg("q"))
	if cmd == nil {
		t.Fatal("q returned no command")
	}
	if _, ok := cmd().(tea.QuitMsg); !ok {
		t.Fatal("q should quit")
	}
}
