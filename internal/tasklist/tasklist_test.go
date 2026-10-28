package tasklist

import (
	"errors"
	"strings"
	"testing"
)

const design = `# Design

Intro paragraph with a fake task: - [ ] not a task.

## Rollout

- [x] Ship the parser
- [ ] Build the view
  - [~] Live reload
  - [ ] Key bindings
- [ ] Wire up Rex

` + "```md\n- [ ] inside a fence\n```\n" + `
### Rollout notes

1. [ ] Ordered task
2. [X] Ordered done

## Appendix

- plain bullet
- [ ] Appendix task
`

func mustSection(t *testing.T, d *Document, name string) Section {
	t.Helper()
	sec, err := d.Section(name)
	if err != nil {
		t.Fatalf("Section(%q): %v", name, err)
	}
	return sec
}

func mustFind(t *testing.T, d *Document, sec Section, q string) Item {
	t.Helper()
	it, err := d.Find(sec, q)
	if err != nil {
		t.Fatalf("Find(%q): %v", q, err)
	}
	return it
}

func texts(items []Item) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Text
	}
	return out
}

func TestRoundTripPreservesBytes(t *testing.T) {
	inputs := map[string]string{
		"empty":            "",
		"newline only":     "\n",
		"design":           design,
		"crlf":             "# T\r\n\r\n- [ ] a\r\n  - [x] b\r\n",
		"no final newline": "- [ ] a\n- [ ] b",
		"tabs and spaces":  "-\t[ ]\ttabbed  \n\t- [~] nested with tab   \n",
		"trailing blanks":  "text\n\n\n",
	}
	for name, src := range inputs {
		t.Run(name, func(t *testing.T) {
			if got := string(Parse([]byte(src)).Bytes()); got != src {
				t.Fatalf("round trip changed bytes\n got %q\nwant %q", got, src)
			}
		})
	}
}

func TestParseItems(t *testing.T) {
	d := Parse([]byte(design))
	items := d.Items(d.Whole())

	want := []struct {
		text  string
		depth int
		state State
	}{
		{"Ship the parser", 0, Done},
		{"Build the view", 0, Todo},
		{"Live reload", 1, InProgress},
		{"Key bindings", 1, Todo},
		{"Wire up Rex", 0, Todo},
		{"Ordered task", 0, Todo},
		{"Ordered done", 0, Done},
		{"Appendix task", 0, Todo},
	}
	if len(items) != len(want) {
		t.Fatalf("got %d items %q, want %d", len(items), texts(items), len(want))
	}
	for i, w := range want {
		it := items[i]
		if it.Text != w.text || it.Depth != w.depth || it.State != w.state {
			t.Errorf("item %d = {%q depth %d %v}, want {%q depth %d %v}",
				i, it.Text, it.Depth, it.State, w.text, w.depth, w.state)
		}
	}
	if p := items[2].Parent; p != items[1].Index {
		t.Errorf("Live reload parent = %d, want %d", p, items[1].Index)
	}
}

func TestPlainBulletEndsNesting(t *testing.T) {
	d := Parse([]byte("- [ ] A\n- note\n  - [ ] B\n"))
	b := mustFind(t, d, d.Whole(), "B")
	if b.Depth != 0 || b.Parent != -1 {
		t.Fatalf("B depth %d parent %d, want top level", b.Depth, b.Parent)
	}
}

func TestMarginParagraphEndsList(t *testing.T) {
	d := Parse([]byte("- [ ] A\nParagraph\n  - [ ] B\n"))
	b := mustFind(t, d, d.Whole(), "B")
	if b.Parent != -1 {
		t.Fatalf("B parent %d, want -1", b.Parent)
	}
}

func TestIndentedFenceHidesTasks(t *testing.T) {
	src := "- [ ] Outer\n  ```\n  - [ ] hidden\n  ```\n  - [ ] Inner\n"
	d := Parse([]byte(src))
	got := texts(d.Items(d.Whole()))
	if strings.Join(got, ",") != "Outer,Inner" {
		t.Fatalf("items %q, want Outer,Inner", got)
	}
	inner := mustFind(t, d, d.Whole(), "Inner")
	if inner.Depth != 1 {
		t.Fatalf("Inner depth %d, want 1: an indented fence must not end the list", inner.Depth)
	}
}

func TestSection(t *testing.T) {
	d := Parse([]byte(design))

	for _, name := range []string{"Rollout", "rollout", "  ROLLOUT "} {
		sec := mustSection(t, d, name)
		got := texts(d.Items(sec))
		if len(got) != 7 {
			t.Errorf("Section(%q) items %q, want 7 including the subsection", name, got)
		}
	}

	notes := mustSection(t, d, "rollout-notes")
	if got := texts(d.Items(notes)); strings.Join(got, ",") != "Ordered task,Ordered done" {
		t.Errorf("slug lookup items %q", got)
	}

	_, err := d.Section("Missing")
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "Rollout notes") {
		t.Errorf("missing heading error = %v, want ErrNotFound listing headings", err)
	}
}

func TestSectionAmbiguous(t *testing.T) {
	d := Parse([]byte("## Plan\n- [ ] a\n## Plan\n- [ ] b\n"))
	var amb *AmbiguousError
	if _, err := d.Section("Plan"); !errors.As(err, &amb) || len(amb.Matches) != 2 {
		t.Fatalf("err = %v, want AmbiguousError with 2 matches", err)
	}
}

func TestFind(t *testing.T) {
	d := Parse([]byte("- [ ] Build\n- [ ] Build the view\n- [ ] Write tests\n- [ ] Write docs\n"))
	sec := d.Whole()

	if got := mustFind(t, d, sec, "build"); got.Text != "Build" {
		t.Errorf("exact match should win, got %q", got.Text)
	}
	if got := mustFind(t, d, sec, "the VIEW"); got.Text != "Build the view" {
		t.Errorf("substring match got %q", got.Text)
	}

	var amb *AmbiguousError
	if _, err := d.Find(sec, "write"); !errors.As(err, &amb) || len(amb.Matches) != 2 {
		t.Errorf("err = %v, want AmbiguousError with 2 matches", err)
	}
	if _, err := d.Find(sec, "deploy"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound", err)
	}
}

func TestSetStateTouchesOnlyTheMarker(t *testing.T) {
	d := Parse([]byte(design))
	it := mustFind(t, d, d.Whole(), "Build the view")
	if err := d.SetState(it, InProgress); err != nil {
		t.Fatal(err)
	}
	want := strings.Replace(design, "- [ ] Build the view", "- [~] Build the view", 1)
	if got := string(d.Bytes()); got != want {
		t.Fatalf("unexpected bytes:\n%s", got)
	}
}

func TestSetStateKeepsUppercaseX(t *testing.T) {
	d := Parse([]byte("- [X] Done\n"))
	if err := d.SetState(mustFind(t, d, d.Whole(), "Done"), Done); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "- [X] Done\n" {
		t.Fatalf("got %q, want the X preserved", got)
	}
}

func TestStaleItemIsRejected(t *testing.T) {
	d := Parse([]byte("- [ ] a\n- [ ] b\n"))
	b := mustFind(t, d, d.Whole(), "b")
	if _, err := d.Insert(mustFind(t, d, d.Whole(), "a"), "new", false); err != nil {
		t.Fatal(err)
	}
	if err := d.SetState(b, Done); !errors.Is(err, ErrStale) {
		t.Fatalf("err = %v, want ErrStale after the line moved", err)
	}
}

func TestSetText(t *testing.T) {
	d := Parse([]byte("  * [x] Old text   \n"))
	if err := d.SetText(mustFind(t, d, d.Whole(), "old"), "  New text "); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "  * [x] New text\n" {
		t.Fatalf("got %q", got)
	}
	if err := d.SetText(mustFind(t, d, d.Whole(), "new"), "two\nlines"); err == nil {
		t.Fatal("multi-line text should be rejected")
	}
}

func TestInsertSiblingFollowsSubtree(t *testing.T) {
	d := Parse([]byte("- [ ] A\n  - [ ] A1\n- [ ] B\n"))
	if _, err := d.Insert(mustFind(t, d, d.Whole(), "A"), "New", false); err != nil {
		t.Fatal(err)
	}
	want := "- [ ] A\n  - [ ] A1\n- [ ] New\n- [ ] B\n"
	if got := string(d.Bytes()); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInsertChild(t *testing.T) {
	cases := map[string]struct{ src, after, want string }{
		"first child of dash": {
			"- [ ] A\n", "A", "- [ ] A\n  - [ ] New\n",
		},
		"first child of ordered": {
			"3. [ ] A\n", "A", "3. [ ] A\n   1. [ ] New\n",
		},
		"copies existing child style": {
			"- [ ] A\n    * [ ] A1\n", "A", "- [ ] A\n    * [ ] A1\n    * [ ] New\n",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := Parse([]byte(c.src))
			it, err := d.Insert(mustFind(t, d, d.Whole(), c.after), "New", true)
			if err != nil {
				t.Fatal(err)
			}
			if got := string(d.Bytes()); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if it.Text != "New" || it.Depth != 1 {
				t.Fatalf("returned item %q depth %d", it.Text, it.Depth)
			}
		})
	}
}

func TestInsertOrderedSiblingNumbers(t *testing.T) {
	d := Parse([]byte("1. [ ] A\n2. [ ] B\n"))
	if _, err := d.Insert(mustFind(t, d, d.Whole(), "A"), "New", false); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "1. [ ] A\n2. [ ] New\n2. [ ] B\n" {
		t.Fatalf("got %q", got)
	}
}

func TestAppend(t *testing.T) {
	cases := map[string]struct{ src, section, want string }{
		"after existing tasks": {
			"## Plan\n- [ ] A\n  - [ ] A1\n\n## Next\n", "Plan",
			"## Plan\n- [ ] A\n  - [ ] A1\n- [ ] New\n\n## Next\n",
		},
		"empty section before heading": {
			"## Plan\n## Next\n", "Plan",
			"## Plan\n- [ ] New\n\n## Next\n",
		},
		"after a paragraph": {
			"## Plan\nSome text.\n\n## Next\n", "Plan",
			"## Plan\nSome text.\n\n- [ ] New\n\n## Next\n",
		},
		"empty document": {
			"", "", "- [ ] New\n",
		},
		"no final newline": {
			"- [ ] A", "", "- [ ] A\n- [ ] New",
		},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			d := Parse([]byte(c.src))
			it, err := d.Append(mustSection(t, d, c.section), "New")
			if err != nil {
				t.Fatal(err)
			}
			if got := string(d.Bytes()); got != c.want {
				t.Fatalf("got %q, want %q", got, c.want)
			}
			if it.Text != "New" {
				t.Fatalf("returned item %q", it.Text)
			}
		})
	}
}

func TestDeleteRemovesSubtree(t *testing.T) {
	d := Parse([]byte("- [ ] A\n  - [ ] A1\n    - [ ] A1a\n- [ ] B\n"))
	if err := d.Delete(mustFind(t, d, d.Whole(), "A")); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "- [ ] B\n" {
		t.Fatalf("got %q", got)
	}
}

func TestDeleteLastLineKeepsEndingStyle(t *testing.T) {
	d := Parse([]byte("- [ ] A\n- [ ] B"))
	if err := d.Delete(mustFind(t, d, d.Whole(), "B")); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "- [ ] A" {
		t.Fatalf("got %q, want no final newline", got)
	}
}

func TestMove(t *testing.T) {
	src := "- [ ] A\n  - [ ] A1\n\n- [ ] B\n- [ ] C"
	d := Parse([]byte(src))
	if err := d.Move(mustFind(t, d, d.Whole(), "B"), -1); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "- [ ] B\n\n- [ ] A\n  - [ ] A1\n- [ ] C" {
		t.Fatalf("move up got %q", got)
	}

	d = Parse([]byte(src))
	if err := d.Move(mustFind(t, d, d.Whole(), "B"), 1); err != nil {
		t.Fatal(err)
	}
	if got := string(d.Bytes()); got != "- [ ] A\n  - [ ] A1\n\n- [ ] C\n- [ ] B" {
		t.Fatalf("move down got %q", got)
	}

	d = Parse([]byte(src))
	if err := d.Move(mustFind(t, d, d.Whole(), "A"), -1); !errors.Is(err, ErrCannotMove) {
		t.Fatalf("err = %v, want ErrCannotMove", err)
	}
	if err := d.Move(mustFind(t, d, d.Whole(), "A1"), 1); !errors.Is(err, ErrCannotMove) {
		t.Fatalf("only child moved: err = %v", err)
	}
}

func TestMoveStaysInsideItsList(t *testing.T) {
	d := Parse([]byte("## One\n- [ ] A\n## Two\n- [ ] B\n"))
	if err := d.Move(mustFind(t, d, d.Whole(), "A"), 1); !errors.Is(err, ErrCannotMove) {
		t.Fatalf("err = %v, want ErrCannotMove across headings", err)
	}
}

func TestProgress(t *testing.T) {
	d := Parse([]byte(design))
	if done, total := d.Progress(mustSection(t, d, "Rollout")); done != 2 || total != 7 {
		t.Fatalf("progress %d/%d, want 2/7", done, total)
	}
}

func TestRefSurvivesInsertions(t *testing.T) {
	d := Parse([]byte("- [ ] Dup\n- [ ] Other\n- [ ] Dup\n"))
	sec := d.Whole()
	second := d.Items(sec)[2]
	ref := d.RefOf(sec, second)
	if ref.Occurrence != 1 {
		t.Fatalf("occurrence %d, want 1", ref.Occurrence)
	}

	if _, err := d.Insert(d.Items(sec)[0], "Inserted", false); err != nil {
		t.Fatal(err)
	}
	got, err := d.Locate(d.Whole(), ref)
	if err != nil {
		t.Fatal(err)
	}
	if got.Line != 3 {
		t.Fatalf("located line %d, want 3", got.Line)
	}
}
