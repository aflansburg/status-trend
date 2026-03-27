package ui

import (
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

type Pane struct {
	Title   string
	Content string
	Offset  int
	Width   int
	Height  int
	Focused bool
}

func (p *Pane) SetContent(content string) {
	p.Content = content
	lines := strings.Split(p.Content, "\n")
	maxOffset := len(lines) - p.contentHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	if p.Offset > maxOffset {
		p.Offset = maxOffset
	}
}

func (p *Pane) ScrollUp(n int) {
	p.Offset -= n
	if p.Offset < 0 {
		p.Offset = 0
	}
}

func (p *Pane) ScrollDown(n int) {
	lines := strings.Split(p.Content, "\n")
	maxOffset := len(lines) - p.contentHeight()
	if maxOffset < 0 {
		maxOffset = 0
	}
	p.Offset += n
	if p.Offset > maxOffset {
		p.Offset = maxOffset
	}
}

func (p *Pane) contentHeight() int {
	h := p.Height - 3 // border-top(1) + title(1) + border-bottom(1)
	if h < 1 {
		h = 1
	}
	return h
}

func (p *Pane) innerWidth() int {
	w := p.Width - 4 // border(2) + padding(2)
	if w < 1 {
		w = 1
	}
	return w
}

func (p *Pane) canScrollUp() bool {
	return p.Offset > 0
}

func (p *Pane) canScrollDown() bool {
	lines := strings.Split(p.Content, "\n")
	return len(lines) > p.contentHeight()+p.Offset
}

// padOrTruncate ensures a string is exactly targetWidth visual characters.
// Truncates if too wide, pads with spaces if too narrow.
func padOrTruncate(s string, targetWidth int) string {
	w := ansi.StringWidth(s)
	if w > targetWidth {
		return ansi.Truncate(s, targetWidth, "")
	}
	if w < targetWidth {
		return s + strings.Repeat(" ", targetWidth-w)
	}
	return s
}

// View renders the pane as exactly p.Height lines, each exactly p.Width visual characters.
func (p *Pane) View() string {
	ch := p.contentHeight()
	cw := p.innerWidth()

	// Border characters
	tl, tr, bl, br, h, v := "╭", "╮", "╰", "╯", "─", "│"

	// Colors
	borderColor := colorBorder
	if p.Focused {
		borderColor = colorCyan
	}
	bc := lipgloss.NewStyle().Foreground(borderColor)

	// Title bar: ╭─ Title ▼───────╮
	scrollHint := ""
	if p.canScrollUp() && p.canScrollDown() {
		scrollHint = " ▲▼"
	} else if p.canScrollUp() {
		scrollHint = " ▲"
	} else if p.canScrollDown() {
		scrollHint = " ▼"
	}

	var titleStyled string
	if p.Focused {
		titleStyled = lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render(p.Title)
	} else {
		titleStyled = lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render(p.Title)
	}
	hintStyled := lipgloss.NewStyle().Foreground(colorDim).Render(scrollHint)

	titleContent := " " + titleStyled + hintStyled + " "
	titleVisualWidth := ansi.StringWidth(titleContent)
	fillWidth := p.Width - 2 - titleVisualWidth // -2 for corners
	if fillWidth < 0 {
		fillWidth = 0
	}
	topLine := bc.Render(tl+h) + titleContent + bc.Render(strings.Repeat(h, fillWidth)+tr)
	topLine = padOrTruncate(topLine, p.Width)

	// Content lines
	lines := strings.Split(p.Content, "\n")
	end := p.Offset + ch
	if end > len(lines) {
		end = len(lines)
	}
	start := p.Offset
	if start > len(lines) {
		start = len(lines)
	}
	visible := lines[start:end]
	for len(visible) < ch {
		visible = append(visible, "")
	}

	// Build content rows: │ content │
	// Inner content area = cw chars, bordered by │ + space on each side = cw + 4 = p.Width
	rows := make([]string, ch)
	for i, line := range visible {
		// Force content to exactly cw width
		line = padOrTruncate(line, cw)
		row := bc.Render(v) + " " + line + " " + bc.Render(v)
		rows[i] = padOrTruncate(row, p.Width)
	}

	// Bottom border: ╰────────────────╯
	bottomFill := p.Width - 2
	if bottomFill < 0 {
		bottomFill = 0
	}
	botLine := bc.Render(bl + strings.Repeat(h, bottomFill) + br)
	botLine = padOrTruncate(botLine, p.Width)

	// Assemble: exactly p.Height lines, each exactly p.Width wide
	out := make([]string, 0, p.Height)
	out = append(out, topLine)
	out = append(out, rows...)
	out = append(out, botLine)

	return strings.Join(out, "\n")
}
