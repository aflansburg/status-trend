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
	vendorIdx  int
	regionIdx  int  // -1 = All Regions, 0+ = index into data.Regions
	simplified bool // true when showing 2-pane view (region with no incidents)
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
		regionIdx: -1, // All Regions
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
	m.regionIdx = -1 // reset to All Regions
	for i := range m.panes {
		m.panes[i].Offset = 0
	}
	return fetchData(m.fetcher)
}

func (m *Model) hasRegions() bool {
	return m.data != nil && len(m.data.Regions) > 0
}

func (m *Model) cycleRegion(delta int) {
	if !m.hasRegions() {
		return
	}
	// range: -1 (All) through len(regions)-1
	count := len(m.data.Regions)
	m.regionIdx += delta
	if m.regionIdx < -1 {
		m.regionIdx = count - 1
	} else if m.regionIdx >= count {
		m.regionIdx = -1
	}
	for i := range m.panes {
		m.panes[i].Offset = 0
	}
	m.updatePaneContent()
	// Clamp focus if we switched to simplified mode
	if m.simplified && m.focusIdx >= 2 {
		m.panes[m.focusIdx].Focused = false
		m.focusIdx = 0
		m.panes[0].Focused = true
	}
}

func (m *Model) selectedRegionCode() string {
	if m.regionIdx < 0 || m.data == nil || m.regionIdx >= len(m.data.Regions) {
		return ""
	}
	return m.data.Regions[m.regionIdx].Code
}

func (m *Model) filteredIncidents() []api.Incident {
	if m.data == nil {
		return nil
	}
	all := m.data.Incidents.Incidents
	code := m.selectedRegionCode()
	if code == "" {
		return all
	}
	var filtered []api.Incident
	for _, inc := range all {
		if inc.RegionCode == code {
			filtered = append(filtered, inc)
		}
	}
	return filtered
}

func (m *Model) filteredComponents() []api.Component {
	if m.data == nil {
		return nil
	}
	code := m.selectedRegionCode()
	if code == "" {
		return m.data.Summary.Components
	}
	return api.BuildAWSComponentsForRegion(m.data.Incidents.Incidents, code)
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

	// title(1) + vendor bar(1) + optional region bar(1) + footer(1)
	chrome := 3
	if m.hasRegions() {
		chrome = 4
	}
	bodyH := m.height - chrome
	if bodyH < 6 {
		bodyH = 6
	}

	if m.simplified {
		// 2-pane layout: Component Health (left) | Summary (right), full height
		m.panes[0].Width = leftW
		m.panes[0].Height = bodyH
		m.panes[1].Width = rightW
		m.panes[1].Height = bodyH
	} else {
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
}

func (m *Model) paneAt(x, y int) int {
	// Grid starts at Y=2 (title + vendor bar) or Y=3 (+ region bar)
	gridStart := 2
	if m.hasRegions() {
		gridStart = 3
	}
	gridY := y - gridStart
	if gridY < 0 {
		return -1
	}

	leftW := m.panes[0].Width
	isLeft := x < leftW

	if m.simplified {
		if isLeft {
			return 0
		}
		return 1
	}

	topH := m.panes[0].Height
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
			maxPane := numPanes
			if m.simplified {
				maxPane = 2
			}
			m.focusIdx = (m.focusIdx + 1) % maxPane
			m.panes[m.focusIdx].Focused = true
			return m, nil
		case "shift+tab":
			m.panes[m.focusIdx].Focused = false
			maxPane := numPanes
			if m.simplified {
				maxPane = 2
			}
			m.focusIdx = (m.focusIdx - 1 + maxPane) % maxPane
			m.panes[m.focusIdx].Focused = true
			return m, nil
		case "[":
			m.cycleRegion(-1)
			return m, nil
		case "]":
			m.cycleRegion(1)
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

	incidents := m.filteredIncidents()
	components := m.filteredComponents()

	// AWS (and any vendor with regions) always uses simplified 2-pane layout
	// since we don't have real historical data for trends
	wasSimplified := m.simplified
	m.simplified = m.hasRegions()
	if m.simplified != wasSimplified {
		m.layoutPanes()
	}

	if m.simplified {
		m.panes[0].Title = "Component Health"
		m.panes[0].SetContent(renderComponents(components))

		if m.regionIdx < 0 {
			// All Regions: show region overview
			m.panes[1].Title = "Region Overview"
			m.panes[1].SetContent(renderRegionOverview(m.data.Regions, m.data.Incidents.Incidents))
		} else if len(incidents) > 0 {
			// Specific region with incidents
			region := m.data.Regions[m.regionIdx]
			m.panes[1].Title = region.Name + " (" + region.Code + ")"
			m.panes[1].SetContent(renderRegionIncidents(incidents))
		} else {
			// Specific region, no incidents
			region := m.data.Regions[m.regionIdx]
			m.panes[1].Title = "Region Summary"
			m.panes[1].SetContent(renderRegionSummary(region))
		}
		return
	}

	// Standard 4-pane view for non-region vendors
	m.panes[0].Title = "Component Health"
	m.panes[0].SetContent(renderComponents(components))

	m.panes[1].Title = "Outages & Incidents Over Time"
	chartWidth := m.panes[1].innerWidth() - 4
	if chartWidth < 10 {
		chartWidth = 10
	}
	timeBuckets := renderTimeBuckets(incidents)
	incidentChart := renderIncidentChart(incidents, chartWidth)
	m.panes[1].SetContent(timeBuckets + "\n\n" + incidentChart)

	m.panes[2].SetContent(renderTrends(incidents))

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
		key.Render("  [  ]       ") + dim.Render("Cycle regions (AWS)"),
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
		dim.Render("  Supported: Claude, OpenAI, Google Cloud AI, AWS, Cohere, GitHub, Vercel"),
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

func (m Model) renderRegionBar(maxW int) string {
	if !m.hasRegions() {
		return ""
	}

	label := lipgloss.NewStyle().Foreground(colorDim).Render("Region: ")
	arrows := lipgloss.NewStyle().Foreground(colorDim)

	var parts []string

	// "All Regions" option
	if m.regionIdx == -1 {
		parts = append(parts, lipgloss.NewStyle().
			Bold(true).Foreground(colorCyan).
			Render("[ All Regions ]"))
	} else {
		parts = append(parts, lipgloss.NewStyle().
			Foreground(colorDim).
			Render("  All Regions  "))
	}

	for i, r := range m.data.Regions {
		display := r.Name + " (" + r.Code + ")"
		if i == m.regionIdx {
			parts = append(parts, lipgloss.NewStyle().
				Bold(true).Foreground(colorCyan).
				Render("[ "+display+" ]"))
		} else {
			parts = append(parts, lipgloss.NewStyle().
				Foreground(colorDim).
				Render("  "+display+"  "))
		}
	}

	bar := strings.Join(parts, "")
	return label + arrows.Render("[ ") + bar + arrows.Render(" ]")
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

	// Pane grid
	var grid string
	if m.simplified {
		grid = lipgloss.JoinHorizontal(lipgloss.Top,
			m.panes[0].View(),
			m.panes[1].View(),
		)
	} else {
		topRow := lipgloss.JoinHorizontal(lipgloss.Top,
			m.panes[0].View(),
			m.panes[1].View(),
		)
		botRow := lipgloss.JoinHorizontal(lipgloss.Top,
			m.panes[2].View(),
			m.panes[3].View(),
		)
		grid = lipgloss.JoinVertical(lipgloss.Left, topRow, botRow)
	}

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

	helpText := "q quit  r refresh  ←→ vendor  tab panes  ↑↓ scroll  h help"
	if m.hasRegions() {
		helpText = "q quit  r refresh  ←→ vendor  [] region  tab panes  ↑↓ scroll  h help"
	}
	help := lipgloss.NewStyle().Foreground(colorDim).Render(helpText)

	footerPad := maxW - lipgloss.Width(stats) - lipgloss.Width(help)
	if footerPad < 1 {
		footerPad = 1
	}
	footer := stats + strings.Repeat(" ", footerPad) + help

	// Build: title + vendor bar + optional region bar + grid, truncate, then append footer
	upper := titleBar + "\n" + vendorBar
	if regionBar := m.renderRegionBar(maxW); regionBar != "" {
		upper += "\n" + regionBar
	}
	upper += "\n" + grid
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
