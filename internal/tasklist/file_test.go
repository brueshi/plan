package tasklist

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func useTempLocks(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	prev := lockDir
	lockDir = func() (string, error) { return dir, nil }
	t.Cleanup(func() { lockDir = prev })
}

func writeFile(t *testing.T, content string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "PLAN.md")
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatal(err)
	}
	return path
}

func setState(query string, s State) func(*Document) error {
	return func(d *Document) error {
		it, err := d.Find(d.Whole(), query)
		if err != nil {
			return err
		}
		return d.SetState(it, s)
	}
}

func TestUpdateWritesAndKeepsMode(t *testing.T) {
	useTempLocks(t)
	path := writeFile(t, "# Plan\n- [ ] A\n", 0o640)

	if _, err := Update(path, setState("A", Done)); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "# Plan\n- [x] A\n" {
		t.Fatalf("file = %q", got)
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm != 0o640 {
		t.Fatalf("mode %v, want 0640", perm)
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Fatalf("temp files left behind: %v", entries)
	}
}

func TestUpdateSkipsNoopWrites(t *testing.T) {
	useTempLocks(t)
	path := writeFile(t, "- [x] A\n", 0o644)
	old := time.Now().Add(-time.Hour).Truncate(time.Second)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	if _, err := Update(path, setState("A", Done)); err != nil {
		t.Fatal(err)
	}
	info, _ := os.Stat(path)
	if !info.ModTime().Equal(old) {
		t.Fatalf("modification time moved on a no-op update")
	}
}

func TestUpdateErrorLeavesFileAlone(t *testing.T) {
	useTempLocks(t)
	path := writeFile(t, "- [ ] A\n", 0o644)
	if _, err := Update(path, setState("missing", Done)); err == nil {
		t.Fatal("want an error for a missing item")
	}
	got, _ := os.ReadFile(path)
	if string(got) != "- [ ] A\n" {
		t.Fatalf("file changed after a failed update: %q", got)
	}
}

func TestUpdateWritesThroughSymlink(t *testing.T) {
	useTempLocks(t)
	target := writeFile(t, "- [ ] A\n", 0o644)
	link := filepath.Join(t.TempDir(), "link.md")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	if _, err := Update(link, setState("A", Done)); err != nil {
		t.Fatal(err)
	}
	if info, _ := os.Lstat(link); info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("symlink was replaced by a regular file")
	}
	if got, _ := os.ReadFile(target); string(got) != "- [x] A\n" {
		t.Fatalf("target = %q", got)
	}
}

func TestConcurrentUpdatesKeepEveryChange(t *testing.T) {
	useTempLocks(t)
	const n = 20
	var src strings.Builder
	for i := range n {
		fmt.Fprintf(&src, "- [ ] task %02d\n", i)
	}
	path := writeFile(t, src.String(), 0o644)

	var wg sync.WaitGroup
	errs := make(chan error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := Update(path, setState(fmt.Sprintf("task %02d", i), Done))
			errs <- err
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	doc, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if done, total := doc.Progress(doc.Whole()); done != n || total != n {
		t.Fatalf("progress %d/%d: a concurrent update was lost", done, total)
	}
}
