package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"status-trend/internal/api"
)

const numPanes = 4

type state int

const (
	stateLoading state = iota
	stateDashboard
	stateError
	stateHelp
)

type Model struct {
	state     state
	data      *api.DashboardData
	err       error
	fetcher   api.Fetcher
	width     int
	height    int
	spinner   int
	tickTime  time.Time
	panes     [numPanes]Pane
	focusIdx  int
	vendorIdx int
}

// Messages
type dataMsg struct{ data *api.DashboardData }
type errMsg struct{ err error }
type tickMsg time.Time

func NewModel() Model {
	m := Model{
		state:     stateLoading,
		fetcher:   api.Vendors[0].NewFetcher(),
		width:     100,
		height:    40,
		vendorIdx: 0,
	}
	m.panes[0] = Pane{Title: "Component Health"}
	m.panes[1] = Pane{Title: "Outages & Incidents Over Time"}
	m.panes[2] = Pane{Title: "Trends"}
	m.panes[3] = Pane{Title: "Impact & Recent Incidents"}
	m.panes[0].Focused = true
	return m
}

func (m *Model) currentVendor() api.Vendor {
	return api.Vendors[m.vendorIdx]
}

func (m *Model) switchVendor(idx int) tea.Cmd {
	m.vendorIdx = idx
	m.fetcher = api.Vendors[idx].NewFetcher()
	m.state = stateLoading
	m.data = nil
	for i := range m.panes {
		m.panes[i].Offset = 0
	}
	return fetchData(m.fetcher)
}

func (m *Model) layoutPanes() {
	maxW := m.width
	if maxW > 160 {
		maxW = 160
	}

	leftW := maxW * 2 / 5
	if leftW < 56 {
		leftW = 56
	}
	if leftW > maxW-30 {
		leftW = maxW - 30
	}
	rightW := maxW - leftW

	// title(1) + vendor bar(1) + footer(1) = 3 lines of chrome
	bodyH := m.height - 3
	if bodyH < 6 {
		bodyH = 6
	}
	topH := bodyH / 2
	botH := bodyH - topH

	// Top row: Component Health (left) | Outages & Incidents (right)
	m.panes[0].Width = leftW
	m.panes[0].Height = topH

	m.panes[1].Width = rightW
	m.panes[1].Height = topH

	// Bottom row: Trends (left) | Impact & Recent (right)
	m.panes[2].Width = leftW
	m.panes[2].Height = botH

	m.panes[3].Width = rightW
	m.panes[3].Height = botH
}

func (m *Model) paneAt(x, y int) int {
	// Grid starts at Y=2 (title + vendor bar)
	gridY := y - 2
	if gridY < 0 {
		return -1
	}

	leftW := m.panes[0].Width
	topH := m.panes[0].Height

	isLeft := x < leftW
	isTop := gridY < topH

	switch {
	case isTop && isLeft:
		return 0
	case isTop && !isLeft:
		return 1
	case !isTop && isLeft:
		return 2
	case !isTop && !isLeft:
		return 3
	}
	return -1
}

func (m *Model) vendorAt(x int) int {
	// Matches renderVendorBar layout: "◀ " + vendors + " ▶"
	pos := 2 // "◀ " = 2 chars
	for i, v := range api.Vendors {
		var labelWidth int
		if i == m.vendorIdx {
			labelWidth = len("[ "+v.Name+" ]")
		} else {
			labelWidth = len("  "+v.Name+"  ")
		}
		if x >= pos && x < pos+labelWidth {
			return i
		}
		pos += labelWidth
	}
	return -1
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(fetchData(m.fetcher), tickCmd())
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.layoutPanes()
		if m.data != nil {
			m.updatePaneContent()
		}
		return m, nil

	case tea.KeyMsg:
		// Help overlay toggle
		if m.state == stateHelp {
			switch msg.String() {
			case "h", "esc", "q", "ctrl+c":
				m.state = stateDashboard
				return m, nil
			}
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "h", "?":
			m.state = stateHelp
			return m, nil
		case "r":
			m.state = stateLoading
			return m, fetchData(m.fetcher)
		case "left":
			idx := (m.vendorIdx - 1 + len(api.Vendors)) % len(api.Vendors)
			return m, m.switchVendor(idx)
		case "right":
			idx := (m.vendorIdx + 1) % len(api.Vendors)
			return m, m.switchVendor(idx)
		case "tab":
			m.panes[m.focusIdx].Focused = false
			m.focusIdx = (m.focusIdx + 1) % numPanes
			m.panes[m.focusIdx].Focused = true
			return m, nil
		case "shift+tab":
			m.panes[m.focusIdx].Focused = false
			m.focusIdx = (m.focusIdx - 1 + numPanes) % numPanes
			m.panes[m.focusIdx].Focused = true
			return m, nil
		case "up", "k":
			m.panes[m.focusIdx].ScrollUp(1)
			return m, nil
		case "down", "j":
			m.panes[m.focusIdx].ScrollDown(1)
			return m, nil
		}

	case tea.MouseClickMsg:
		if m.state != stateDashboard {
			return m, nil
		}
		mouse := msg.Mouse()
		// Vendor bar is on Y=1
		if mouse.Y == 1 {
			if vendorIdx := m.vendorAt(mouse.X); vendorIdx >= 0 && vendorIdx != m.vendorIdx {
				return m, m.switchVendor(vendorIdx)
			}
			return m, nil
		}
		// Pane click
		if paneIdx := m.paneAt(mouse.X, mouse.Y); paneIdx >= 0 {
			m.panes[m.focusIdx].Focused = false
			m.focusIdx = paneIdx
			m.panes[m.focusIdx].Focused = true
		}
		return m, nil

	case tea.MouseWheelMsg:
		if m.state != stateDashboard {
			return m, nil
		}
		mouse := msg.Mouse()
		if paneIdx := m.paneAt(mouse.X, mouse.Y); paneIdx >= 0 {
			if paneIdx != m.focusIdx {
				m.panes[m.focusIdx].Focused = false
				m.focusIdx = paneIdx
				m.panes[m.focusIdx].Focused = true
			}
			if mouse.Button == tea.MouseWheelUp {
				m.panes[m.focusIdx].ScrollUp(3)
			} else if mouse.Button == tea.MouseWheelDown {
				m.panes[m.focusIdx].ScrollDown(3)
			}
		}
		return m, nil

	case dataMsg:
		m.data = msg.data
		m.state = stateDashboard
		m.layoutPanes()
		m.updatePaneContent()
		return m, nil

	case errMsg:
		m.err = msg.err
		m.state = stateError
		return m, nil

	case tickMsg:
		m.spinner++
		m.tickTime = time.Time(msg)
		return m, tickCmd()
	}

	return m, nil
}

func (m *Model) updatePaneContent() {
	if m.data == nil {
		return
	}

	incidents := m.data.Incidents.Incidents

	// Pane 0: Component Health (top-left)
	m.panes[0].SetContent(renderComponents(m.data.Summary.Components))

	// Pane 1: Outages by Time Window + Incidents by Day (top-right)
	chartWidth := m.panes[1].innerWidth() - 4
	if chartWidth < 10 {
		chartWidth = 10
	}
	timeBuckets := renderTimeBuckets(incidents)
	incidentChart := renderIncidentChart(incidents, chartWidth)
	m.panes[1].SetContent(timeBuckets + "\n\n" + incidentChart)

	// Pane 2: Trends (bottom-left)
	m.panes[2].SetContent(renderTrends(incidents))

	// Pane 3: Impact Breakdown + Recent Incidents (bottom-right)
	impact := renderImpactBreakdown(incidents)
	recent := renderRecentIncidents(incidents, 8)
	m.panes[3].SetContent(impact + "\n\n" + recent)
}

func (m Model) View() tea.View {
	var s string
	switch m.state {
	case stateLoading:
		s = m.viewLoading()
	case stateError:
		s = m.viewError()
	case stateHelp:
		s = m.viewHelp()
	default:
		s = m.viewDashboard()
	}
	v := tea.NewView(s)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (m Model) viewLoading() string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	frame := frames[m.spinner%len(frames)]

	vendor := m.currentVendor()
	spinner := lipgloss.NewStyle().Foreground(colorCyan).Render(frame)
	text := lipgloss.NewStyle().Foreground(colorWhite).Render(
		fmt.Sprintf(" Fetching status data from %s...", vendor.Name))

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		spinner+text,
	)
}

func (m Model) viewError() string {
	vendor := m.currentVendor()
	errStr := m.err.Error()

	// Classify the error into a friendly message
	var friendly string
	switch {
	case strings.Contains(errStr, "no such host"):
		friendly = "Could not resolve the status service hostname.\nThis is usually a temporary DNS issue."
	case strings.Contains(errStr, "connection refused"):
		friendly = "The status service refused the connection.\nThe service may be temporarily unavailable."
	case strings.Contains(errStr, "timeout") || strings.Contains(errStr, "deadline exceeded"):
		friendly = "The status service took too long to respond.\nThe service may be experiencing issues."
	case strings.Contains(errStr, "status 429"):
		friendly = "Rate limited by the status service.\nTry again in a few seconds."
	case strings.Contains(errStr, "status 5"):
		friendly = "The status service returned a server error.\nThe service may be experiencing issues."
	default:
		friendly = "Could not reach the status service."
	}

	title := lipgloss.NewStyle().
		Foreground(colorRed).
		Bold(true).
		Render(vendor.Name + " status service did not respond")

	message := lipgloss.NewStyle().
		Foreground(colorWhite).
		Render(friendly)

	detail := lipgloss.NewStyle().
		Foreground(colorDim).
		Render("Details: " + errStr)

	hint := helpStyle.Render("r retry  ←→ switch vendor  q quit")

	content := title + "\n\n" + message + "\n\n" + detail + "\n\n" + hint

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		content,
	)
}

func (m Model) viewHelp() string {
	title := lipgloss.NewStyle().Bold(true).Foreground(colorCyan).Render("STATUS-TREND HELP")
	dim := lipgloss.NewStyle().Foreground(colorDim)
	bold := lipgloss.NewStyle().Bold(true).Foreground(colorWhite)
	key := lipgloss.NewStyle().Bold(true).Foreground(colorCyan)
	section := lipgloss.NewStyle().Bold(true).Foreground(colorOrange)

	lines := []string{
		title,
		"",
		section.Render("NAVIGATION"),
		key.Render("  ←  →       ") + dim.Render("Switch between vendors"),
		key.Render("  tab        ") + dim.Render("Cycle focus to next pane"),
		key.Render("  shift+tab  ") + dim.Render("Cycle focus to previous pane"),
		key.Render("  ↑  ↓  j  k ") + dim.Render("Scroll focused pane"),
		key.Render("  r          ") + dim.Render("Refresh data from current vendor"),
		key.Render("  h  ?       ") + dim.Render("Toggle this help screen"),
		key.Render("  q  ctrl+c  ") + dim.Render("Quit"),
		"",
		section.Render("PANES"),
		bold.Render("  Component Health") + dim.Render("  Live status of each service component"),
		bold.Render("  Outages          ") + dim.Render("  Time-window counts + daily bar chart"),
		bold.Render("  Trends           ") + dim.Render("  Trend analysis (see below)"),
		bold.Render("  Impact & Recent  ") + dim.Render("  Severity breakdown + latest incidents"),
		"",
		section.Render("TRENDS PANE"),
		"",
		bold.Render("  Reliability (8 weeks)"),
		dim.Render("    Sparkline of weekly incident counts over 8 weeks."),
		dim.Render("    Color: ") +
			lipgloss.NewStyle().Foreground(colorGreen).Render("green") + dim.Render("=0  ") +
			lipgloss.NewStyle().Foreground(colorYellow).Render("yellow") + dim.Render("=1-2  ") +
			lipgloss.NewStyle().Foreground(colorOrange).Render("orange") + dim.Render("=3-5  ") +
			lipgloss.NewStyle().Foreground(colorRed).Render("red") + dim.Render("=6+"),
		dim.Render("    Arrow compares last 2 weeks vs prior 2 weeks."),
		"",
		bold.Render("  MTTR (Mean Time to Resolve)"),
		dim.Render("    Average resolution time per week for the last 4 weeks."),
		dim.Render("    Shorter bars = faster fixes. Color-coded by duration:"),
		dim.Render("    ") +
			lipgloss.NewStyle().Foreground(colorGreen).Render("<30m") + dim.Render("  ") +
			lipgloss.NewStyle().Foreground(colorYellow).Render("30m-2h") + dim.Render("  ") +
			lipgloss.NewStyle().Foreground(colorOrange).Render("2h-6h") + dim.Render("  ") +
			lipgloss.NewStyle().Foreground(colorRed).Render(">6h"),
		"",
		bold.Render("  Week over Week"),
		dim.Render("    Incident count this week vs last week with delta."),
		dim.Render("    ") +
			lipgloss.NewStyle().Foreground(colorRed).Render("▲ +N") + dim.Render(" = more incidents (worse)  ") +
			lipgloss.NewStyle().Foreground(colorGreen).Render("▼ -N") + dim.Render(" = fewer (better)"),
		"",
		bold.Render("  Severity Trend (2-week)"),
		dim.Render("    Ratio of severe (critical+major) vs mild (minor+none)"),
		dim.Render("    incidents over last 2 weeks compared to prior 2 weeks."),
		dim.Render("    ") +
			lipgloss.NewStyle().Foreground(colorRed).Render("▰ severe") + dim.Render("  ") +
			lipgloss.NewStyle().Foreground(colorYellow).Render("▰ mild"),
		"",
		section.Render("VENDORS"),
		dim.Render("  Data is fetched live from each vendor's public status API."),
		dim.Render("  Incident history covers the last 56 days (8 weeks) for full trend coverage."),
		dim.Render("  Supported: Claude, OpenAI, Google Cloud AI, Cohere, GitHub, Vercel"),
		"",
		lipgloss.NewStyle().Foreground(colorDim).Italic(true).Render("  Press h, ?, or esc to return to the dashboard"),
	}

	content := strings.Join(lines, "\n")

	box := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(colorCyan).
		Padding(1, 3).
		Render(content)

	return lipgloss.Place(m.width, m.height,
		lipgloss.Center, lipgloss.Center,
		box,
	)
}

func (m Model) renderVendorBar(maxW int) string {
	var parts []string
	for i, v := range api.Vendors {
		name := v.Name
		if i == m.vendorIdx {
			parts = append(parts, lipgloss.NewStyle().
				Bold(true).
				Foreground(colorCyan).
				Render("[ "+name+" ]"))
		} else {
			parts = append(parts, lipgloss.NewStyle().
				Foreground(colorDim).
				Render("  "+name+"  "))
		}
	}

	bar := strings.Join(parts, "")
	arrows := lipgloss.NewStyle().Foreground(colorDim).Render("◀ ")
	arrowsR := lipgloss.NewStyle().Foreground(colorDim).Render(" ▶")

	return arrows + bar + arrowsR
}

func (m Model) viewDashboard() string {
	if m.data == nil {
		return ""
	}

	maxW := m.width
	if maxW > 160 {
		maxW = 160
	}

	vendor := m.currentVendor()

	// Title bar
	statusDesc := m.data.Summary.Status.Description
	statusCol := statusColor(m.data.Summary.Status.Indicator + "_outage")
	if m.data.Summary.Status.Indicator == "none" {
		statusCol = colorGreen
	}
	liveIndicator := lipgloss.NewStyle().Foreground(statusCol).Bold(true).Render("●")
	statusText := lipgloss.NewStyle().Foreground(statusCol).Bold(true).Render(statusDesc)

	titleLeft := titleStyle.Render(strings.ToUpper(vendor.Name) + " STATUS DASHBOARD")
	titleRight := fmt.Sprintf("%s %s  %s",
		liveIndicator,
		statusText,
		lipgloss.NewStyle().Foreground(colorDim).Render(m.data.FetchedAt.Format("15:04:05")),
	)
	titlePad := maxW - lipgloss.Width(titleLeft) - lipgloss.Width(titleRight)
	if titlePad < 1 {
		titlePad = 1
	}
	titleBar := titleLeft + strings.Repeat(" ", titlePad) + titleRight

	// Vendor selector bar
	vendorBar := m.renderVendorBar(maxW)

	// Pane grid (2x2)
	topRow := lipgloss.JoinHorizontal(lipgloss.Top,
		m.panes[0].View(),
		m.panes[1].View(),
	)
	botRow := lipgloss.JoinHorizontal(lipgloss.Top,
		m.panes[2].View(),
		m.panes[3].View(),
	)
	grid := lipgloss.JoinVertical(lipgloss.Left, topRow, botRow)

	// Footer
	activeCount := 0
	for _, inc := range m.data.Incidents.Incidents {
		if inc.Status != "resolved" && inc.Status != "postmortem" {
			activeCount++
		}
	}

	stats := lipgloss.NewStyle().Foreground(colorDim).Render(
		fmt.Sprintf(" Active: %s",
			activeIndicator.Render(fmt.Sprintf("%d", activeCount)),
		))

	help := lipgloss.NewStyle().Foreground(colorDim).Render(
		"q quit  r refresh  ←→ vendor  tab panes  ↑↓ scroll  h help")

	footerPad := maxW - lipgloss.Width(stats) - lipgloss.Width(help)
	if footerPad < 1 {
		footerPad = 1
	}
	footer := stats + strings.Repeat(" ", footerPad) + help

	// Build: title + vendor bar + grid, truncate, then append footer
	upper := titleBar + "\n" + vendorBar + "\n" + grid
	lines := strings.Split(upper, "\n")
	maxUpperLines := m.height - 1
	if len(lines) > maxUpperLines {
		lines = lines[:maxUpperLines]
	}

	lines = append(lines, footer)
	return strings.Join(lines, "\n")
}

func fetchData(fetcher api.Fetcher) tea.Cmd {
	return func() tea.Msg {
		data, err := fetcher.FetchAll()
		if err != nil {
			return errMsg{err}
		}
		return dataMsg{data}
	}
}

func tickCmd() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
