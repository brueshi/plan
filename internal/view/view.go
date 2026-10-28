// Package view is the interactive checklist: a Bubble Tea model over one
// plan file, optionally narrowed to a heading. Every edit is a fresh,
// locked read-modify-write of the file, and outside changes are picked up
// by polling, so the view and agents can edit the same plan at once.
package view

import (
	"errors"
	"os"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/brueshi/plan/internal/tasklist"
)

// pollInterval is how often the file is checked for outside edits.
const pollInterval = 300 * time.Millisecond

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
	path    string
	section string // heading name, empty for the whole file

	doc   *tasklist.Document
	sec   tasklist.Section
	items []tasklist.Item
	info  os.FileInfo // nil forces a reload on the next poll

	cursor int
	offset int // first visible row
	mode   mode
	input  textinput.Model
	chord  string // first key of dd or gg
	err    error

	width, height int
}

type tickMsg struct{}

// loadedMsg carries a new revision of the plan, from an edit or from
// a poll that saw the file change.
type loadedMsg struct {
	doc   *tasklist.Document
	info  os.FileInfo
	focus *tasklist.Ref // item to put the cursor on
	err   error
}

// New loads the plan at path, narrowed to section when it isn't empty.
func New(path, section string) (Model, error) {
	info, err := os.Stat(path)
	if err != nil {
		return Model{}, err
	}
	doc, err := tasklist.Load(path)
	if err != nil {
		return Model{}, err
	}
	sec, err := doc.Section(section)
	if err != nil {
		return Model{}, err
	}

	input := textinput.New()
	styles := input.Styles()
	// A steady cursor keeps the input free of timers.
	styles.Cursor.Blink = false
	input.SetStyles(styles)

	return Model{
		path:    path,
		section: section,
		doc:     doc,
		sec:     sec,
		items:   doc.Items(sec),
		info:    info,
		input:   input,
	}, nil
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
	sec, err := msg.doc.Section(m.section)
	if err != nil {
		m.err = err
		return m
	}
	m.doc, m.sec, m.items, m.info, m.err = msg.doc, sec, msg.doc.Items(sec), msg.info, nil
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

// checkFile reloads the plan when its file changed since the last load.
func (m Model) checkFile() tea.Cmd {
	path, prev, focus := m.path, m.info, m.focusRef()
	return func() tea.Msg {
		info, err := os.Stat(path)
		if err != nil {
			return loadedMsg{err: err}
		}
		if prev != nil && os.SameFile(prev, info) &&
			info.ModTime().Equal(prev.ModTime()) && info.Size() == prev.Size() {
			return nil
		}
		doc, err := tasklist.Load(path)
		if err != nil {
			return loadedMsg{err: err}
		}
		return loadedMsg{doc: doc, info: info, focus: focus}
	}
}

func (m Model) focusRef() *tasklist.Ref {
	if len(m.items) == 0 {
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
	m.err = nil
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
