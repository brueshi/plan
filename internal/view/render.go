package view

import (
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/brueshi/plan/internal/tasklist"
)

const (
	headerRows  = 2 // title, rule
	footerRows  = 2 // rule, help or input
	progressBar = 10
	cursorMark  = "▸ "
	indentStep  = "  "
)

// Basic ANSI colors follow the terminal's own theme,
// so the view fits light and dark setups alike.
var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	dimStyle      = lipgloss.NewStyle().Faint(true)
	doneStyle     = lipgloss.NewStyle().Faint(true).Strikethrough(true)
	doingStyle    = lipgloss.NewStyle().Foreground(lipgloss.Yellow)
	cursorStyle   = lipgloss.NewStyle().Foreground(lipgloss.Blue).Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true)
	errStyle      = lipgloss.NewStyle().Foreground(lipgloss.Red)
	noticeStyle   = lipgloss.NewStyle().Foreground(lipgloss.Green)
)

const helpText = "space done · ~ doing · a add · A sub · e edit · dd del · J/K move · s send · q quit"

// row is one rendered line of the list and the item it belongs to.
type row struct {
	item int
	text string
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.ProgramStatus = m.programStatus()
	return v
}

// programStatus reports progress to Rex. An open plan reports idle,
// not working, so the tab spinner keeps meaning "an agent is busy".
// Done is reported only when the last task is checked while open.
func (m Model) programStatus() *tea.ProgramStatus {
	done, total := m.doc.Progress(m.sec)
	ps := &tea.ProgramStatus{
		App:     "plan",
		Title:   m.title(),
		State:   tea.ProgramStateIdle,
		Message: fmt.Sprintf("%d of %d tasks done", done, total),
	}
	if m.finished {
		ps.State = tea.ProgramStateDone
		ps.Message = fmt.Sprintf("All %d tasks done", total)
	}
	return ps
}

func (m Model) title() string {
	title := filepath.Base(m.path)
	if m.sec.Heading != "" {
		title += " › " + m.sec.Heading
	}
	return title
}

func (m Model) render() string {
	var b strings.Builder
	b.WriteString(m.header())
	b.WriteByte('\n')
	b.WriteString(m.rule())
	b.WriteByte('\n')

	rows := m.rows()
	visible := m.visibleRows()
	for i := m.offset; i < len(rows) && i < m.offset+visible; i++ {
		b.WriteString(rows[i].text)
		b.WriteByte('\n')
	}
	if len(m.items) == 0 {
		b.WriteString(dimStyle.Render("  No tasks yet. Press a to add one."))
		b.WriteByte('\n')
	}

	// Pin the footer to the bottom of the screen.
	if m.height > 0 {
		used := headerRows + min(len(rows), visible) + footerRows
		if len(m.items) == 0 {
			used++
		}
		b.WriteString(strings.Repeat("\n", max(m.height-used, 0)))
	}
	b.WriteString(m.rule())
	b.WriteByte('\n')
	b.WriteString(m.footer())
	return b.String()
}

func (m Model) header() string {
	title := m.title()
	done, total := m.doc.Progress(m.sec)
	progress := fmt.Sprintf("%d/%d", done, total)
	if total > 0 {
		filled := done * progressBar / total
		progress += " " + strings.Repeat("▓", filled) + strings.Repeat("░", progressBar-filled)
	}

	if m.width <= 0 {
		return titleStyle.Render(title) + "  " + progress
	}
	room := m.width - lipgloss.Width(progress) - 2
	if room < lipgloss.Width(title) {
		title = truncate(title, max(room, 1))
	}
	gap := max(m.width-lipgloss.Width(title)-lipgloss.Width(progress), 1)
	return titleStyle.Render(title) + strings.Repeat(" ", gap) + progress
}

func (m Model) rule() string {
	return dimStyle.Render(strings.Repeat("─", max(m.width, 1)))
}

func (m Model) footer() string {
	switch {
	case m.mode == confirmingDelete:
		return errStyle.Render(truncate(fmt.Sprintf(
			"Delete %q and its subtasks? y/n", m.items[m.cursor].Text), m.width))
	case m.mode != normal:
		return m.input.View()
	case m.err != nil:
		return errStyle.Render(truncate(m.errText(), m.width))
	case m.notice != "":
		return noticeStyle.Render(truncate(m.notice, m.width))
	}
	return dimStyle.Render(truncate(helpText, m.width))
}

// errText favors short messages: the footer is one line,
// often in a narrow pane.
func (m Model) errText() string {
	if errors.Is(m.err, fs.ErrNotExist) {
		return filepath.Base(m.path) + " was moved or deleted"
	}
	return m.err.Error()
}

// rows lays out every item, wrapping long text under its first line.
func (m Model) rows() []row {
	var rows []row
	for i, it := range m.items {
		lead := strings.Repeat(indentStep, it.Depth) + marker(it.State) + " "
		textWidth := 0
		if m.width > 0 {
			textWidth = max(m.width-lipgloss.Width(cursorMark)-lipgloss.Width(lead), 1)
		}
		lines := []string{it.Text}
		if textWidth > 0 {
			lines = strings.Split(lipgloss.Wrap(it.Text, textWidth, " "), "\n")
		}

		style := lipgloss.NewStyle()
		switch it.State {
		case tasklist.Done:
			style = doneStyle
		case tasklist.InProgress:
			style = doingStyle
		}
		if i == m.cursor {
			style = style.Inherit(selectedStyle)
		}

		for n, text := range lines {
			prefix := "  "
			if n == 0 && i == m.cursor {
				prefix = cursorStyle.Render(cursorMark)
			}
			// Wrapped lines hang under the text, not the marker.
			pad := strings.Repeat(" ", lipgloss.Width(lead))
			if n == 0 {
				pad = strings.Repeat(indentStep, it.Depth) + markerStyle(it.State).Render(marker(it.State)) + " "
			}
			rows = append(rows, row{item: i, text: prefix + pad + style.Render(text)})
		}
	}
	return rows
}

func marker(s tasklist.State) string {
	return "[" + string(byte(s)) + "]"
}

func markerStyle(s tasklist.State) lipgloss.Style {
	switch s {
	case tasklist.Done:
		return dimStyle
	case tasklist.InProgress:
		return doingStyle
	}
	return lipgloss.NewStyle()
}

func (m Model) visibleRows() int {
	if m.height <= 0 {
		return 1 << 30
	}
	return max(m.height-headerRows-footerRows, 1)
}

// scrolled moves the window only as far as needed to show the whole
// cursor item, so the list stays put while the cursor moves inside it.
func (m Model) scrolled() Model {
	rows := m.rows()
	first, last := -1, -1
	for i, r := range rows {
		if r.item == m.cursor {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	if first < 0 {
		m.offset = 0
		return m
	}
	visible := m.visibleRows()
	if last >= m.offset+visible {
		m.offset = last - visible + 1
	}
	if first < m.offset {
		m.offset = first
	}
	m.offset = max(min(m.offset, len(rows)-visible), 0)
	return m
}

func truncate(s string, width int) string {
	if width <= 0 || lipgloss.Width(s) <= width {
		return s
	}
	runes := []rune(s)
	for len(runes) > 0 && lipgloss.Width(string(runes))+1 > width {
		runes = runes[:len(runes)-1]
	}
	return string(runes) + "…"
}
