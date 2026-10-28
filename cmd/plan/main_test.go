package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// env gives each test its own state dir and Rex session,
// and a working directory with no plan in it.
func env(t *testing.T, session string) {
	t.Helper()
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	t.Setenv("REX_SESSION", session)
	t.Chdir(t.TempDir())
}

func runOK(t *testing.T, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	if err := run(args, &out); err != nil {
		t.Fatalf("plan %s: %v", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(out.String())
}

func TestNewMakesTheScratchPlanActive(t *testing.T) {
	env(t, "session:test")
	path := runOK(t, "new", "Auth", "refactor")
	if !strings.HasSuffix(path, "-auth-refactor.md") {
		t.Fatalf("path %q", path)
	}
	if got := runOK(t, "which"); got != path {
		t.Fatalf("which = %q, want the new scratch plan %q", got, path)
	}
}

func TestUseThenWhich(t *testing.T) {
	env(t, "session:test")
	plan := filepath.Join(t.TempDir(), "design.md")
	if err := os.WriteFile(plan, []byte("## Rollout\n- [ ] A\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	runOK(t, "use", plan+"#rollout")
	if got := runOK(t, "which"); got != plan+"#rollout" {
		t.Fatalf("which = %q", got)
	}
	if err := run([]string{"use", plan + "#missing"}, &bytes.Buffer{}); err == nil {
		t.Fatal("use accepted a heading that isn't there")
	}
}

func TestWhichWithoutAPlanExplainsWhatToDo(t *testing.T) {
	env(t, "session:test")
	err := run([]string{"which"}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `plan new "Title"`) {
		t.Fatalf("err = %v, want guidance on starting a plan", err)
	}
}

func TestNewOutsideRexStillCreates(t *testing.T) {
	env(t, "")
	path := runOK(t, "new", "Loose")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("scratch plan not created: %v", err)
	}
}

func TestHelp(t *testing.T) {
	if out := runOK(t, "--help"); !strings.Contains(out, "plan use FILE") {
		t.Fatalf("help = %q", out)
	}
}
