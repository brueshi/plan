// Package active decides which plan a command works on: the one a Rex
// session has marked active, or PLAN.md at the root of the current repo.
// Session pointers and scratch plans live in a state directory outside
// any repo.
package active

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/brueshi/plan/internal/tasklist"
)

const defaultPlan = "PLAN.md"

// ErrNoPlan means nothing names a plan for this session or directory.
var ErrNoPlan = errors.New("no plan here")

// Target is a plan file, optionally narrowed to one heading.
type Target struct {
	Path    string
	Section string
}

func (t Target) String() string {
	if t.Section == "" {
		return t.Path
	}
	return t.Path + "#" + t.Section
}

// Parse splits FILE#HEADING. A file whose own name
// contains # is taken whole.
func Parse(s string) Target {
	if _, err := os.Stat(s); err == nil {
		return Target{Path: s}
	}
	i := strings.LastIndex(s, "#")
	if i < 0 {
		return Target{Path: s}
	}
	return Target{Path: s[:i], Section: s[i+1:]}
}

// Check confirms the file exists and the heading is in it.
func (t Target) Check() error {
	doc, err := tasklist.Load(t.Path)
	if err != nil {
		return err
	}
	_, err = doc.Section(t.Section)
	return err
}

// stateDir follows the XDG base directory spec, which
// command-line tools use on macOS as well as Linux.
func stateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "plan"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "plan"), nil
}

var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func pointerPath(session string) (string, error) {
	if session == "" {
		return "", errors.New("not in a Rex session: REX_SESSION is not set")
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "sessions", unsafeChars.ReplaceAllString(session, "_")), nil
}

// Use makes t the session's active plan.
func Use(session string, t Target) error {
	if err := t.Check(); err != nil {
		return err
	}
	abs, err := filepath.Abs(t.Path)
	if err != nil {
		return err
	}
	ptr, err := pointerPath(session)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(ptr), 0o700); err != nil {
		return err
	}
	return os.WriteFile(ptr, []byte(Target{Path: abs, Section: t.Section}.String()+"\n"), 0o600)
}

// Current returns the session's active plan. ok is false when there
// is none, or when its file has since been moved or deleted.
func Current(session string) (t Target, ok bool, err error) {
	ptr, err := pointerPath(session)
	if err != nil {
		return Target{}, false, err
	}
	b, err := os.ReadFile(ptr)
	if errors.Is(err, fs.ErrNotExist) {
		return Target{}, false, nil
	}
	if err != nil {
		return Target{}, false, err
	}
	t = Parse(strings.TrimSpace(string(b)))
	if _, err := os.Stat(t.Path); err != nil {
		return Target{}, false, nil
	}
	return t, true, nil
}

// Resolve finds the plan for a session and directory: the session's
// active plan, then PLAN.md at the repo root, then in dir itself.
func Resolve(session, dir string) (Target, error) {
	return (&Resolver{Session: session, Dir: dir}).Resolve()
}

// Resolver resolves repeatedly for one session and directory, as a
// view following the active plan does several times a second. It runs
// git once: a pane's directory never changes, so neither does its repo.
type Resolver struct {
	Session string
	Dir     string

	rootOnce sync.Once
	root     string
}

// Resolve works like the package-level Resolve.
func (r *Resolver) Resolve() (Target, error) {
	if r.Session != "" {
		if t, ok, err := Current(r.Session); err != nil || ok {
			return t, err
		}
	}
	r.rootOnce.Do(func() { r.root = repoRoot(r.Dir) })
	for _, base := range []string{r.root, r.Dir} {
		if base == "" {
			continue
		}
		path := filepath.Join(base, defaultPlan)
		if _, err := os.Stat(path); err == nil {
			return Target{Path: path}, nil
		}
	}
	return Target{}, ErrNoPlan
}

func repoRoot(dir string) string {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// NewScratch creates a plan with no home in a repo and returns its path.
// The name starts with the date so a listing reads in order.
func NewScratch(title string, now time.Time) (string, error) {
	title = strings.TrimSpace(title)
	if title == "" || strings.ContainsAny(title, "\r\n") {
		return "", errors.New("a scratch plan needs a one-line title")
	}
	dir, err := stateDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "scratch")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}

	slug := strings.Trim(unsafeChars.ReplaceAllString(strings.ToLower(title), "-"), "-._")
	if slug == "" {
		slug = "plan"
	}
	base := now.Format("2006-01-02") + "-" + slug
	content := []byte("# " + title + "\n\n")
	for n := 1; ; n++ {
		name := base + ".md"
		if n > 1 {
			name = fmt.Sprintf("%s-%d.md", base, n)
		}
		path := filepath.Join(dir, name)
		// O_EXCL never overwrites a plan with the same name.
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		_, werr := f.Write(content)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		return path, werr
	}
}
