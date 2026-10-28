// Package view is the interactive checklist: a Bubble Tea model over one
// plan file, optionally narrowed to a heading. Every edit is a fresh,
// locked read-modify-write of the file, and outside changes are picked up
// by polling, so the view and agents can edit the same plan at once.
//
// A view can also follow the session's active plan, switching when an
// agent starts a new one, and waiting when there is none yet.
package view

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/brueshi/plan/internal/active"
	"github.com/brueshi/plan/internal/rexctl"
	"github.com/brueshi/plan/internal/tasklist"
)

// writePlanPrompt asks the agent to turn the plan from its conversation
// into a scratch plan. plan new makes it the session's active plan,
// which a following view then switches to.
const writePlanPrompt = "Write the plan we agreed on to a checklist I can watch: " +
	"run `plan new \"<short title>\"`, then write one `- [ ]` task per stage into " +
	"the file it prints, with the steps as indented subtasks. Mark finished " +
	"stages `[x]` and the current one `[~]`, and keep it updated as you finish each step."

const (
	// pollInterval is how often the file is checked for outside edits.
	pollInterval = 300 * time.Millisecond
	// sendTimeout bounds the rex calls behind one send.
	sendTimeout = 5 * time.Second
)

type mode int

const (
	normal mode = iota
	adding
	addingChild
	editing
	confirmingDelete
)

// Model is the checklist view.
type Model struct {
	path    string // empty while waiting for a plan
	section string // heading name, empty for the whole file

	// follow names the plan to show. Nil fixes the view on path.
	follow func() (active.Target, error)

	doc   *tasklist.Document // nil while waiting for a plan
	sec   tasklist.Section
	items []tasklist.Item
	info  os.FileInfo // nil forces a reload on the next poll

	cursor int
	offset int // first visible row
	mode   mode
	input  textinput.Model
	chord  string // first key of dd or gg
	err    error
	notice string // a confirmation, shown until the next key

	// finished is set when the last open task is checked while the
	// view is open, so opening a completed plan doesn't announce it.
	finished bool

	rexSession, rexBlock string
	runRex               rexctl.Runner

	width, height int
}

type tickMsg struct{}

type sentMsg struct {
	notice string
	err    error
}

// loadedMsg carries a new revision of the plan, from an edit or from
// a poll that saw the file change.
type loadedMsg struct {
	doc     *tasklist.Document
	info    os.FileInfo
	focus   *tasklist.Ref  // item to put the cursor on
	target  *active.Target // the plan doc came from, when polled
	waiting bool           // the followed plan is gone
	err     error
}

func newModel() Model {
	input := textinput.New()
	styles := input.Styles()
	// A steady cursor keeps the input free of timers.
	styles.Cursor.Blink = false
	input.SetStyles(styles)

	return Model{
		input:      input,
		rexSession: os.Getenv("REX_SESSION"),
		rexBlock:   os.Getenv("REX_BLOCK"),
		runRex:     rexctl.CLI,
	}
}

// New shows the plan at path, narrowed to section when it isn't empty.
func New(path, section string) (Model, error) {
	m := newModel().loaded(load(active.Target{Path: path, Section: section}, nil))
	if m.err != nil {
		return Model{}, m.err
	}
	return m, nil
}

// Follow shows the plan resolve names and switches when that changes,
// as when an agent runs plan new. With no plan yet it shows a waiting
// screen rather than failing, so it can be opened first.
func Follow(resolve func() (active.Target, error)) Model {
	m := newModel()
	m.follow = resolve
	if msg, ok := m.checkFile()().(loadedMsg); ok {
		m = m.loaded(msg)
	}
	return m
}

func load(t active.Target, focus *tasklist.Ref) loadedMsg {
	info, err := os.Stat(t.Path)
	if err != nil {
		return loadedMsg{err: err}
	}
	doc, err := tasklist.Load(t.Path)
	if err != nil {
		return loadedMsg{err: err}
	}
	return loadedMsg{doc: doc, info: info, focus: focus, target: &t}
}

func (m Model) Init() tea.Cmd {
	return poll()
}

func poll() tea.Cmd {
	return tea.Tick(pollInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		m.input.SetWidth(max(msg.Width-lipgloss.Width(m.prompt())-2, 1))
		return m.scrolled(), nil
	case tickMsg:
		return m, tea.Batch(m.checkFile(), poll())
	case loadedMsg:
		return m.loaded(msg).scrolled(), nil
	case sentMsg:
		m.err, m.notice = msg.err, msg.notice
		if msg.err != nil {
			m.notice = ""
		}
		return m, nil
	case tea.KeyPressMsg:
		switch m.mode {
		case normal:
			return m.normalKey(msg.String())
		case confirmingDelete:
			return m.confirmKey(msg.String())
		default:
			return m.inputKey(msg)
		}
	}
	if m.mode == adding || m.mode == addingChild || m.mode == editing {
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) loaded(msg loadedMsg) Model {
	if msg.err != nil {
		m.err = msg.err
		return m
	}
	if msg.waiting {
		m = m.reset()
		m.path, m.section = "", ""
		return m
	}

	section := m.section
	if msg.target != nil {
		section = msg.target.Section
	}
	sec, err := msg.doc.Section(section)
	if err != nil {
		m.err = err
		return m
	}
	if t := msg.target; t != nil && (t.Path != m.path || t.Section != m.section) {
		m = m.reset()
		m.path, m.section = t.Path, t.Section
	}

	prevDone, prevTotal := 0, 0
	if m.doc != nil {
		prevDone, prevTotal = m.doc.Progress(m.sec)
	}
	m.doc, m.sec, m.items, m.info, m.err = msg.doc, sec, msg.doc.Items(sec), msg.info, nil
	done, total := m.doc.Progress(m.sec)
	switch {
	case total == 0 || done < total:
		m.finished = false
	case prevDone < prevTotal:
		m.finished = true
	}
	if msg.focus != nil {
		if it, err := m.doc.Locate(m.sec, *msg.focus); err == nil {
			m.cursor = m.indexOf(it)
		}
	}
	m.cursor = min(max(m.cursor, 0), max(len(m.items)-1, 0))
	return m
}

func (m Model) indexOf(it tasklist.Item) int {
	for i, c := range m.items {
		if c.Line == it.Line {
			return i
		}
	}
	return m.cursor
}

// reset clears everything tied to the current plan,
// for a switch to another plan or to waiting.
func (m Model) reset() Model {
	if m.mode != normal {
		// Never let a half-typed edit land in another plan.
		m = m.endInput()
	}
	m.doc, m.items, m.info = nil, nil, nil
	m.sec = tasklist.Section{}
	m.cursor, m.offset, m.chord, m.finished = 0, 0, "", false
	return m
}

// checkFile reloads the plan when its file changed since the last load,
// and, when following, switches when a different plan becomes active.
func (m Model) checkFile() tea.Cmd {
	path, section, prev, focus, follow := m.path, m.section, m.info, m.focusRef(), m.follow
	return func() tea.Msg {
		if follow != nil {
			t, err := follow()
			switch {
			case errors.Is(err, active.ErrNoPlan):
				if path == "" {
					return nil
				}
				return loadedMsg{waiting: true}
			case err != nil:
				return loadedMsg{err: err}
			case t.Path != path || t.Section != section:
				return load(t, nil)
			}
		}
		info, err := os.Stat(path)
		if err != nil {
			return loadedMsg{err: err}
		}
		if prev != nil && os.SameFile(prev, info) &&
			info.ModTime().Equal(prev.ModTime()) && info.Size() == prev.Size() {
			return nil
		}
		return load(active.Target{Path: path, Section: section}, focus)
	}
}

func (m Model) focusRef() *tasklist.Ref {
	if m.doc == nil || len(m.items) == 0 {
		return nil
	}
	ref := m.doc.RefOf(m.sec, m.items[m.cursor])
	return &ref
}

// edit applies op to a fresh read of the file. op returns the item the
// cursor should land on, or nil to keep the cursor where it is.
func (m Model) edit(op func(*tasklist.Document, tasklist.Section) (*tasklist.Item, error)) tea.Cmd {
	path, section := m.path, m.section
	return func() tea.Msg {
		var focus *tasklist.Ref
		doc, err := tasklist.Update(path, func(d *tasklist.Document) error {
			sec, err := d.Section(section)
			if err != nil {
				return err
			}
			it, err := op(d, sec)
			if err != nil || it == nil {
				return err
			}
			// The edit moved lines, so the old section is stale.
			if sec, err = d.Section(section); err != nil {
				return err
			}
			ref := d.RefOf(sec, *it)
			focus = &ref
			return nil
		})
		// No file info: another writer may land between our write and
		// a stat, so the next poll reloads to be sure.
		return loadedMsg{doc: doc, focus: focus, err: err}
	}
}

// onCursor runs op on the item under the cursor, found again by
// reference in the fresh read.
func (m Model) onCursor(op func(*tasklist.Document, tasklist.Item) (*tasklist.Item, error)) tea.Cmd {
	ref := m.focusRef()
	if ref == nil {
		return nil
	}
	return m.edit(func(d *tasklist.Document, sec tasklist.Section) (*tasklist.Item, error) {
		target, err := d.Locate(sec, *ref)
		if err != nil {
			return nil, err
		}
		return op(d, target)
	})
}

func (m Model) normalKey(key string) (tea.Model, tea.Cmd) {
	m.err, m.notice = nil, ""
	if m.doc == nil {
		switch key {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "w":
			return m, m.sendToAgent(writePlanPrompt,
				"Asked claude to write its plan. It appears here once written.")
		}
		return m, nil
	}
	if pending := m.chord; pending != "" {
		m.chord = ""
		switch {
		case pending == "d" && key == "d":
			if m.hasChildren() {
				m.mode = confirmingDelete
				return m, nil
			}
			return m, m.deleteCursor()
		case pending == "g" && key == "g":
			m.cursor = 0
			return m.scrolled(), nil
		}
	}

	switch key {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "j", "down":
		m.cursor = min(m.cursor+1, max(len(m.items)-1, 0))
	case "k", "up":
		m.cursor = max(m.cursor-1, 0)
	case "G":
		m.cursor = max(len(m.items)-1, 0)
	case "d", "g":
		if len(m.items) > 0 || key == "g" {
			m.chord = key
		}
	case "space", "x":
		return m, m.toggle(tasklist.Done)
	case "~":
		return m, m.toggle(tasklist.InProgress)
	case "s":
		return m, m.send()
	case "J":
		return m, m.move(1)
	case "K":
		return m, m.move(-1)
	case "a":
		return m.startInput(adding, ""), nil
	case "A":
		if len(m.items) == 0 {
			return m.startInput(adding, ""), nil
		}
		return m.startInput(addingChild, ""), nil
	case "e", "enter":
		if len(m.items) > 0 {
			return m.startInput(editing, m.items[m.cursor].Text), nil
		}
	}
	return m.scrolled(), nil
}

// toggle sets state, or clears it back to todo when
// the item already has it.
func (m Model) toggle(state tasklist.State) tea.Cmd {
	return m.onCursor(func(d *tasklist.Document, it tasklist.Item) (*tasklist.Item, error) {
		next := state
		if it.State == state {
			next = tasklist.Todo
		}
		return nil, d.SetState(it, next)
	})
}

func (m Model) move(delta int) tea.Cmd {
	return m.onCursor(func(d *tasklist.Document, it tasklist.Item) (*tasklist.Item, error) {
		moved, err := d.Move(it, delta)
		if errors.Is(err, tasklist.ErrCannotMove) {
			return nil, nil
		}
		return &moved, err
	})
}

func (m Model) deleteCursor() tea.Cmd {
	return m.onCursor(func(d *tasklist.Document, it tasklist.Item) (*tasklist.Item, error) {
		return nil, d.Delete(it)
	})
}

// send asks the agent beside this pane to work on the cursor task.
func (m Model) send() tea.Cmd {
	if len(m.items) == 0 {
		return nil
	}
	task := m.items[m.cursor].Text
	message := fmt.Sprintf("Work on this task from the plan at %s: %s", m.planRef(), task)
	return m.sendToAgent(message, "Sent to claude: "+task)
}

// sendToAgent types message into the agent beside this pane, and
// shows notice once it has gone.
func (m Model) sendToAgent(message, notice string) tea.Cmd {
	if m.rexSession == "" {
		return func() tea.Msg {
			return sentMsg{err: errors.New("not running in Rex, so there is no agent to send to")}
		}
	}
	run, session, self := m.runRex, m.rexSession, m.rexBlock
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), sendTimeout)
		defer cancel()
		agent, err := rexctl.FindAgent(ctx, run, session, self)
		if err == nil {
			err = rexctl.Send(ctx, run, session, agent, message)
		}
		return sentMsg{notice: notice, err: err}
	}
}

// planRef is an absolute FILE#HEADING, so the agent can
// find the plan from any working directory.
func (m Model) planRef() string {
	path, err := filepath.Abs(m.path)
	if err != nil {
		path = m.path
	}
	if m.sec.Heading == "" {
		return path
	}
	return path + "#" + m.sec.Heading
}

func (m Model) hasChildren() bool {
	next := m.cursor + 1
	return next < len(m.items) && m.items[next].Parent == m.items[m.cursor].Index
}

func (m Model) confirmKey(key string) (tea.Model, tea.Cmd) {
	m.mode = normal
	if key == "y" {
		return m, m.deleteCursor()
	}
	return m, nil
}

func (m Model) startInput(md mode, value string) Model {
	m.mode = md
	m.input.Prompt = m.promptFor(md)
	m.input.SetValue(value)
	m.input.CursorEnd()
	// Focus only returns a command for a blinking cursor.
	_ = m.input.Focus()
	return m
}

func (m Model) inputKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		return m.endInput(), nil
	case "enter":
		text, md := m.input.Value(), m.mode
		m = m.endInput()
		if text == "" {
			return m, nil
		}
		return m, m.submit(md, text)
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m Model) endInput() Model {
	m.mode = normal
	m.input.Blur()
	m.input.Reset()
	return m
}

func (m Model) submit(md mode, text string) tea.Cmd {
	if md == editing {
		return m.onCursor(func(d *tasklist.Document, it tasklist.Item) (*tasklist.Item, error) {
			edited, err := d.SetText(it, text)
			return &edited, err
		})
	}
	if len(m.items) == 0 {
		return m.edit(func(d *tasklist.Document, sec tasklist.Section) (*tasklist.Item, error) {
			added, err := d.Append(sec, text)
			return &added, err
		})
	}
	return m.onCursor(func(d *tasklist.Document, it tasklist.Item) (*tasklist.Item, error) {
		added, err := d.Insert(it, text, md == addingChild)
		return &added, err
	})
}

func (m Model) prompt() string {
	return m.promptFor(m.mode)
}

func (m Model) promptFor(md mode) string {
	switch md {
	case addingChild:
		return "subtask › "
	case editing:
		return "edit › "
	}
	return "add › "
}
