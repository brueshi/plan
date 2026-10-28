package active

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const session = "session:01a1-test"

func useTempState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_STATE_HOME", dir)
	return filepath.Join(dir, "plan")
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestParse(t *testing.T) {
	dir := t.TempDir()
	hashName := filepath.Join(dir, "notes#1.md")
	write(t, hashName, "")

	cases := []struct{ in, path, section string }{
		{"PLAN.md", "PLAN.md", ""},
		{"docs/design.md#Rollout plan", "docs/design.md", "Rollout plan"},
		{"docs/design.md#rollout-plan", "docs/design.md", "rollout-plan"},
		{hashName, hashName, ""},
		{hashName + "#Intro", hashName, "Intro"},
	}
	for _, c := range cases {
		got := Parse(c.in)
		if got.Path != c.path || got.Section != c.section {
			t.Errorf("Parse(%q) = %+v, want {%q %q}", c.in, got, c.path, c.section)
		}
		if got.String() != c.in {
			t.Errorf("String() = %q, want %q", got.String(), c.in)
		}
	}
}

func TestUseAndCurrent(t *testing.T) {
	useTempState(t)
	plan := filepath.Join(t.TempDir(), "design.md")
	write(t, plan, "## Rollout\n- [ ] A\n")

	if _, ok, err := Current(session); err != nil || ok {
		t.Fatalf("fresh session: ok=%v err=%v, want none", ok, err)
	}
	if err := Use(session, Target{Path: plan, Section: "Rollout"}); err != nil {
		t.Fatal(err)
	}
	got, ok, err := Current(session)
	if err != nil || !ok {
		t.Fatalf("ok=%v err=%v", ok, err)
	}
	if got.Path != plan || got.Section != "Rollout" {
		t.Fatalf("got %+v", got)
	}

	if err := os.Remove(plan); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := Current(session); err != nil || ok {
		t.Fatalf("deleted plan should read as none: ok=%v err=%v", ok, err)
	}
}

func TestUseRejectsBadTargets(t *testing.T) {
	useTempState(t)
	plan := filepath.Join(t.TempDir(), "design.md")
	write(t, plan, "## Rollout\n")

	if err := Use(session, Target{Path: plan, Section: "Nope"}); err == nil {
		t.Error("unknown heading accepted")
	}
	if err := Use(session, Target{Path: plan + ".missing"}); err == nil {
		t.Error("missing file accepted")
	}
	if err := Use("", Target{Path: plan}); err == nil || !strings.Contains(err.Error(), "REX_SESSION") {
		t.Errorf("no session: err = %v", err)
	}
}

func TestUseStoresAbsolutePath(t *testing.T) {
	useTempState(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "PLAN.md"), "")
	t.Chdir(dir)

	if err := Use(session, Target{Path: "PLAN.md"}); err != nil {
		t.Fatal(err)
	}
	got, _, _ := Current(session)
	if !filepath.IsAbs(got.Path) {
		t.Fatalf("stored %q; a relative path breaks from other directories", got.Path)
	}
}

func TestResolveOrder(t *testing.T) {
	useTempState(t)
	repo := t.TempDir()
	if out, err := exec.Command("git", "-C", repo, "init", "-q").CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	sub := filepath.Join(repo, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	if _, err := Resolve(session, sub); !errors.Is(err, ErrNoPlan) {
		t.Fatalf("empty repo: err = %v, want ErrNoPlan", err)
	}

	write(t, filepath.Join(repo, "PLAN.md"), "")
	got, err := Resolve(session, sub)
	if err != nil {
		t.Fatal(err)
	}
	if resolved, _ := filepath.EvalSymlinks(got.Path); resolved != mustEval(t, filepath.Join(repo, "PLAN.md")) {
		t.Fatalf("got %q, want the repo root PLAN.md", got.Path)
	}

	other := filepath.Join(t.TempDir(), "other.md")
	write(t, other, "")
	if err := Use(session, Target{Path: other}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Resolve(session, sub); got.Path != other {
		t.Fatalf("got %q, want the session's active plan first", got.Path)
	}
	if got, _ := Resolve("", sub); got.Path == other {
		t.Fatal("no session should skip the pointer")
	}
}

func TestResolveOutsideRepo(t *testing.T) {
	useTempState(t)
	dir := t.TempDir()
	write(t, filepath.Join(dir, "PLAN.md"), "")
	got, err := Resolve("", dir)
	if err != nil || got.Path != filepath.Join(dir, "PLAN.md") {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestNewScratch(t *testing.T) {
	state := useTempState(t)
	day := time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC)

	first, err := NewScratch("Auth refactor: phase 1", day)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "scratch", "2026-10-28-auth-refactor-phase-1.md")
	if first != want {
		t.Fatalf("path %q, want %q", first, want)
	}
	if b, _ := os.ReadFile(first); string(b) != "# Auth refactor: phase 1\n\n" {
		t.Fatalf("content %q", b)
	}

	second, err := NewScratch("Auth refactor: phase 1", day)
	if err != nil {
		t.Fatal(err)
	}
	if second == first || !strings.HasSuffix(second, "-2.md") {
		t.Fatalf("second scratch %q should not reuse %q", second, first)
	}
	if _, err := NewScratch("  ", day); err == nil {
		t.Fatal("blank title accepted")
	}
}
