// Package tasklist reads and edits GitHub-style task lists inside
// arbitrary markdown. Only task lines are ever rewritten. Every other
// byte of the file, line endings included, is preserved.
package tasklist

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// State is the marker between a task's brackets.
type State byte

const (
	Todo       State = ' '
	InProgress State = '~'
	Done       State = 'x'
)

func (s State) String() string {
	switch s {
	case Todo:
		return "todo"
	case InProgress:
		return "in progress"
	case Done:
		return "done"
	}
	return fmt.Sprintf("State(%q)", byte(s))
}

func parseState(b byte) State {
	switch b {
	case 'x', 'X':
		return Done
	case '~':
		return InProgress
	}
	return Todo
}

var (
	ErrNotFound   = errors.New("not found")
	ErrStale      = errors.New("item no longer matches the document")
	ErrCannotMove = errors.New("no sibling in that direction")
)

// AmbiguousError reports a query that matched more than one thing.
type AmbiguousError struct {
	Query   string
	Matches []string
}

func (e *AmbiguousError) Error() string {
	return fmt.Sprintf("%q matches %d items: %s",
		e.Query, len(e.Matches), strings.Join(e.Matches, "; "))
}

// Item is one task line. Positions are only valid for the document
// revision it came from.
type Item struct {
	Index  int // position in the document's item list
	Line   int // zero-based line number
	Depth  int // 0 for a top-level item
	Parent int // Index of the parent item, or -1
	State  State
	Text   string

	group   int // run of list lines; nesting never crosses runs
	end     int // last line of the item's subtree
	width   int // indentation width, tabs expanded
	indent  string
	bullet  string
	gap     string
	stateAt int // byte offset of the state marker
	closeAt int // byte offset of the closing bracket
}

// Section is a range of lines: the body under a heading, including its
// subsections, or the whole document. Like an Item, it is only valid for
// the document revision it came from, so resolve it again after an edit.
type Section struct {
	Heading string
	Level   int
	Start   int // first line of the body
	End     int // one past the last line
}

type line struct {
	text string // content without its terminator
	eol  string // "\n", "\r\n", or "" for a final unterminated line
}

type heading struct {
	line  int
	level int
	text  string
}

// Document is a parsed markdown file.
type Document struct {
	lines    []line
	eol      string // terminator for inserted lines
	items    []Item
	headings []heading
}

var (
	taskRe     = regexp.MustCompile(`^([ \t]*)([-*+]|[0-9]{1,9}[.)])([ \t]+)\[([ xX~])\](?:[ \t]+(.*?))?[ \t]*$`)
	listItemRe = regexp.MustCompile(`^([ \t]*)(?:[-*+]|[0-9]{1,9}[.)])(?:[ \t]|$)`)
	headingRe  = regexp.MustCompile(`^ {0,3}(#{1,6})[ \t]+(.*?)(?:[ \t]+#+)?[ \t]*$`)
	fenceRe    = regexp.MustCompile("^([ \t]*)(`{3,}|~{3,})")
)

// Parse reads markdown. It never fails: text that isn't a task list is
// carried through untouched.
func Parse(src []byte) *Document {
	d := &Document{lines: splitLines(string(src)), eol: "\n"}
	for _, l := range d.lines {
		if l.eol != "" {
			d.eol = l.eol
			break
		}
	}
	d.index()
	return d
}

func splitLines(s string) []line {
	var lines []line
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			lines = append(lines, line{text: s})
			break
		}
		text, eol := s[:i], "\n"
		if strings.HasSuffix(text, "\r") {
			text, eol = text[:len(text)-1], "\r\n"
		}
		lines = append(lines, line{text: text, eol: eol})
		s = s[i+1:]
	}
	return lines
}

// Bytes returns the document as markdown.
func (d *Document) Bytes() []byte {
	var b strings.Builder
	for _, l := range d.lines {
		b.WriteString(l.text)
		b.WriteString(l.eol)
	}
	return []byte(b.String())
}

func (d *Document) index() {
	d.items, d.headings = nil, nil
	var fence string // opening fence run, empty outside a fence
	var stack []int  // open item indices, innermost last
	group := 0
	endRun := func() { stack, group = nil, group+1 }

	for n, l := range d.lines {
		if fence != "" {
			if closesFence(l.text, fence) {
				fence = ""
			}
			continue
		}
		if m := fenceRe.FindStringSubmatch(l.text); m != nil {
			fence = m[2]
			if m[1] == "" {
				endRun()
			}
			continue
		}
		if m := headingRe.FindStringSubmatch(l.text); m != nil {
			d.headings = append(d.headings, heading{line: n, level: len(m[1]), text: m[2]})
			endRun()
			continue
		}
		if m := listItemRe.FindStringSubmatch(l.text); m != nil {
			// Any list item closes deeper or equal items.
			w := indentWidth(m[1])
			for len(stack) > 0 && d.items[stack[len(stack)-1]].width >= w {
				stack = stack[:len(stack)-1]
			}
		} else if strings.TrimSpace(l.text) != "" && !startsIndented(l.text) {
			// A paragraph at the margin ends the list.
			endRun()
			continue
		}
		loc := taskRe.FindStringSubmatchIndex(l.text)
		if loc == nil {
			continue
		}
		parent := -1
		if len(stack) > 0 {
			parent = stack[len(stack)-1]
		}
		it := Item{
			Index:   len(d.items),
			Line:    n,
			Depth:   len(stack),
			Parent:  parent,
			State:   parseState(l.text[loc[8]]),
			group:   group,
			end:     n,
			indent:  l.text[loc[2]:loc[3]],
			bullet:  l.text[loc[4]:loc[5]],
			gap:     l.text[loc[6]:loc[7]],
			stateAt: loc[8],
			closeAt: loc[9],
		}
		it.width = indentWidth(it.indent)
		if loc[10] >= 0 {
			it.Text = l.text[loc[10]:loc[11]]
		}
		d.items = append(d.items, it)
		stack = append(stack, it.Index)
	}

	// Children follow parents, so a reverse pass sees
	// every descendant before its ancestor.
	for i := len(d.items) - 1; i >= 0; i-- {
		if p := d.items[i].Parent; p >= 0 && d.items[i].end > d.items[p].end {
			d.items[p].end = d.items[i].end
		}
	}
}

func closesFence(text, fence string) bool {
	t := strings.TrimSpace(text)
	if len(t) < len(fence) || t[0] != fence[0] {
		return false
	}
	return strings.Trim(t, string(fence[0])) == ""
}

func startsIndented(text string) bool {
	return strings.HasPrefix(text, " ") || strings.HasPrefix(text, "\t")
}

func indentWidth(s string) int {
	w := 0
	for _, r := range s {
		if r == '\t' {
			w += 4 - w%4
		} else {
			w++
		}
	}
	return w
}

// Whole is the section covering the entire document.
func (d *Document) Whole() Section {
	return Section{Start: 0, End: len(d.lines)}
}

// Section finds a heading by its text, case-insensitively, or by its
// GitHub anchor slug. An empty name is the whole document.
func (d *Document) Section(name string) (Section, error) {
	if strings.TrimSpace(name) == "" {
		return d.Whole(), nil
	}
	want, wantSlug := normalize(name), strings.ToLower(strings.TrimSpace(name))
	var found []heading
	for _, h := range d.headings {
		if normalize(h.text) == want || slug(h.text) == wantSlug {
			found = append(found, h)
		}
	}
	switch len(found) {
	case 0:
		names := make([]string, len(d.headings))
		for i, h := range d.headings {
			names[i] = h.text
		}
		return Section{}, fmt.Errorf("heading %q: %w; headings are: %s",
			name, ErrNotFound, strings.Join(names, "; "))
	case 1:
	default:
		names := make([]string, len(found))
		for i, h := range found {
			names[i] = fmt.Sprintf("%s (line %d)", h.text, h.line+1)
		}
		return Section{}, &AmbiguousError{Query: name, Matches: names}
	}

	h := found[0]
	sec := Section{Heading: h.text, Level: h.level, Start: h.line + 1, End: len(d.lines)}
	for _, next := range d.headings {
		if next.line > h.line && next.level <= h.level {
			sec.End = next.line
			break
		}
	}
	return sec, nil
}

// Items returns the tasks in a section, in document order.
func (d *Document) Items(sec Section) []Item {
	var out []Item
	for _, it := range d.items {
		if it.Line >= sec.Start && it.Line < sec.End {
			out = append(out, it)
		}
	}
	return out
}

// Progress counts finished tasks in a section.
func (d *Document) Progress(sec Section) (done, total int) {
	for _, it := range d.Items(sec) {
		total++
		if it.State == Done {
			done++
		}
	}
	return done, total
}

// Find matches a query against task text. An exact match wins over
// substring matches. More than one match is an AmbiguousError.
func (d *Document) Find(sec Section, query string) (Item, error) {
	q := normalize(query)
	if q == "" {
		return Item{}, errors.New("empty query")
	}
	var exact, partial []Item
	for _, it := range d.Items(sec) {
		t := normalize(it.Text)
		switch {
		case t == q:
			exact = append(exact, it)
		case strings.Contains(t, q):
			partial = append(partial, it)
		}
	}
	for _, set := range [][]Item{exact, partial} {
		switch len(set) {
		case 0:
			continue
		case 1:
			return set[0], nil
		}
		texts := make([]string, len(set))
		for i, it := range set {
			texts[i] = it.Text
		}
		return Item{}, &AmbiguousError{Query: query, Matches: texts}
	}
	return Item{}, fmt.Errorf("item %q: %w", query, ErrNotFound)
}

// Ref names an item by its text and which duplicate it is. Unlike a
// line number, it survives lines being added or moved around the item.
type Ref struct {
	Text       string
	Occurrence int
}

// RefOf returns a Ref for an item in a section.
func (d *Document) RefOf(sec Section, it Item) Ref {
	ref := Ref{Text: it.Text}
	for _, other := range d.Items(sec) {
		if other.Line >= it.Line {
			break
		}
		if other.Text == it.Text {
			ref.Occurrence++
		}
	}
	return ref
}

// Locate finds the item a Ref names.
func (d *Document) Locate(sec Section, ref Ref) (Item, error) {
	seen := 0
	for _, it := range d.Items(sec) {
		if it.Text != ref.Text {
			continue
		}
		if seen == ref.Occurrence {
			return it, nil
		}
		seen++
	}
	return Item{}, fmt.Errorf("item %q: %w", ref.Text, ErrNotFound)
}

// current re-finds an item so edits never apply
// to a line that has changed underneath it.
func (d *Document) current(it Item) (Item, error) {
	for _, c := range d.items {
		if c.Line == it.Line && c.Text == it.Text {
			return c, nil
		}
	}
	return Item{}, fmt.Errorf("%q: %w", it.Text, ErrStale)
}

// SetState rewrites an item's marker. Setting the state it already has
// changes nothing, so an existing "X" stays uppercase.
func (d *Document) SetState(it Item, s State) error {
	if s != Todo && s != InProgress && s != Done {
		return fmt.Errorf("invalid state %q", byte(s))
	}
	c, err := d.current(it)
	if err != nil || c.State == s {
		return err
	}
	t := d.lines[c.Line].text
	d.lines[c.Line].text = t[:c.stateAt] + string(byte(s)) + t[c.stateAt+1:]
	d.index()
	return nil
}

// SetText replaces an item's text, keeping its bullet and marker.
func (d *Document) SetText(it Item, text string) error {
	text, err := cleanText(text)
	if err != nil {
		return err
	}
	c, err := d.current(it)
	if err != nil {
		return err
	}
	d.lines[c.Line].text = d.lines[c.Line].text[:c.closeAt+1] + " " + text
	d.index()
	return nil
}

// Insert adds a todo after an item: as the sibling that follows its
// subtree, or as its last child.
func (d *Document) Insert(after Item, text string, asChild bool) (Item, error) {
	text, err := cleanText(text)
	if err != nil {
		return Item{}, err
	}
	c, err := d.current(after)
	if err != nil {
		return Item{}, err
	}

	indent, bullet, gap := c.indent, nextBullet(c.bullet), c.gap
	if asChild {
		if first, ok := d.firstChild(c); ok {
			indent, bullet, gap = first.indent, first.bullet, first.gap
		} else {
			column := c.width + len(c.bullet) + len(c.gap)
			indent, bullet, gap = strings.Repeat(" ", column), firstBullet(c.bullet), " "
		}
	}
	pos := c.end + 1
	d.insertLines(pos, indent+bullet+gap+"[ ] "+text)
	return d.itemAt(pos)
}

// Append adds a top-level todo at the end of a section's tasks, or
// starts a list there if it has none.
func (d *Document) Append(sec Section, text string) (Item, error) {
	text, err := cleanText(text)
	if err != nil {
		return Item{}, err
	}
	items := d.Items(sec)
	if len(items) > 0 {
		last := items[0]
		for _, it := range items {
			if it.Depth <= last.Depth {
				last = it
			}
		}
		return d.Insert(last, text, false)
	}

	pos := sec.Start
	for i := sec.End - 1; i >= sec.Start; i-- {
		if strings.TrimSpace(d.lines[i].text) != "" {
			pos = i + 1
			break
		}
	}
	newLines := []string{"- [ ] " + text}
	if pos > sec.Start {
		newLines = append([]string{""}, newLines...)
	}
	itemLine := pos + len(newLines) - 1
	if pos < len(d.lines) && strings.TrimSpace(d.lines[pos].text) != "" {
		newLines = append(newLines, "")
	}
	d.insertLines(pos, newLines...)
	return d.itemAt(itemLine)
}

// Delete removes an item and its subtree.
func (d *Document) Delete(it Item) error {
	c, err := d.current(it)
	if err != nil {
		return err
	}
	d.editLines(func(lines []line) []line {
		return append(lines[:c.Line:c.Line], lines[c.end+1:]...)
	})
	return nil
}

// Move swaps an item's subtree with the previous (delta < 0) or next
// (delta > 0) sibling's. Lines between the two stay where they are.
func (d *Document) Move(it Item, delta int) error {
	c, err := d.current(it)
	if err != nil {
		return err
	}
	var sib *Item
	for i := range d.items {
		o := &d.items[i]
		if o.Parent != c.Parent || o.group != c.group || o.Index == c.Index {
			continue
		}
		if delta < 0 && o.Index < c.Index {
			sib = o // keep the nearest one before
		}
		if delta > 0 && o.Index > c.Index {
			sib = o
			break
		}
	}
	if sib == nil {
		return ErrCannotMove
	}
	a, b := c, *sib
	if a.Line > b.Line {
		a, b = b, a
	}
	d.editLines(func(lines []line) []line {
		out := make([]line, 0, len(lines))
		out = append(out, lines[:a.Line]...)
		out = append(out, lines[b.Line:b.end+1]...)
		out = append(out, lines[a.end+1:b.Line]...)
		out = append(out, lines[a.Line:a.end+1]...)
		return append(out, lines[b.end+1:]...)
	})
	return nil
}

func (d *Document) firstChild(it Item) (Item, bool) {
	if next := it.Index + 1; next < len(d.items) && d.items[next].Parent == it.Index {
		return d.items[next], true
	}
	return Item{}, false
}

func (d *Document) itemAt(lineNum int) (Item, error) {
	for _, it := range d.items {
		if it.Line == lineNum {
			return it, nil
		}
	}
	return Item{}, fmt.Errorf("no item on line %d: %w", lineNum+1, ErrNotFound)
}

func (d *Document) insertLines(pos int, texts ...string) {
	d.editLines(func(lines []line) []line {
		added := make([]line, len(texts))
		for i, t := range texts {
			added[i] = line{text: t, eol: d.eol}
		}
		out := append([]line{}, lines[:pos]...)
		out = append(out, added...)
		return append(out, lines[pos:]...)
	})
}

// editLines lets an edit treat every line as terminated, then restores a
// missing final newline so the file's ending style survives the edit.
func (d *Document) editLines(edit func([]line) []line) {
	unterminated := len(d.lines) > 0 && d.lines[len(d.lines)-1].eol == ""
	if unterminated {
		d.lines[len(d.lines)-1].eol = d.eol
	}
	d.lines = edit(d.lines)
	if unterminated && len(d.lines) > 0 {
		d.lines[len(d.lines)-1].eol = ""
	}
	d.index()
}

func cleanText(text string) (string, error) {
	if strings.ContainsAny(text, "\r\n") {
		return "", errors.New("task text must be a single line")
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", errors.New("task text is empty")
	}
	return text, nil
}

// nextBullet numbers an ordered sibling one past
// the item it follows. Other bullets repeat.
func nextBullet(b string) string {
	n, err := strconv.Atoi(b[:len(b)-1])
	if err != nil {
		return b
	}
	return strconv.Itoa(n+1) + b[len(b)-1:]
}

func firstBullet(b string) string {
	if _, err := strconv.Atoi(b[:len(b)-1]); err != nil {
		return b
	}
	return "1" + b[len(b)-1:]
}

func normalize(s string) string {
	return strings.ToLower(strings.Join(strings.Fields(s), " "))
}

// slug follows GitHub's heading anchors: lowercase,
// spaces to hyphens, other punctuation dropped.
func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), r == '_', r == '-':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}
