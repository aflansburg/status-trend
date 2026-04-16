package ui

import (
	"fmt"
	"image/color"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"status-trend/internal/api"
)

var barChars = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

func renderUptimeBar(pct float64, width int) string {
	filled := int(pct / 100.0 * float64(width))
	if filled > width {
		filled = width
	}
	empty := width - filled

	var c color.Color
	switch {
	case pct >= 99.9:
		c = colorGreen
	case pct >= 99.5:
		c = colorYellow
	case pct >= 99.0:
		c = colorOrange
	default:
		c = colorRed
	}

	bar := lipgloss.NewStyle().Foreground(c).Render(strings.Repeat("█", filled))
	bar += lipgloss.NewStyle().Foreground(colorDarkGray).Render(strings.Repeat("░", empty))
	return bar
}

type dayBucket struct {
	Date  string
	Count int
	Max   string // worst impact that day
}

func buildDailyBuckets(incidents []api.Incident, days int) []dayBucket {
	now := time.Now().UTC()
	buckets := make(map[string]*dayBucket)

	for i := 0; i < days; i++ {
		d := now.AddDate(0, 0, -i)
		key := d.Format("2006-01-02")
		buckets[key] = &dayBucket{Date: key, Count: 0, Max: "none"}
	}

	impactSeverity := map[string]int{"none": 0, "minor": 1, "major": 2, "critical": 3}

	for _, inc := range incidents {
		key := inc.CreatedAt.UTC().Format("2006-01-02")
		if b, ok := buckets[key]; ok {
			b.Count++
			if impactSeverity[inc.Impact] > impactSeverity[b.Max] {
				b.Max = inc.Impact
			}
		}
	}

	result := make([]dayBucket, 0, days)
	for _, b := range buckets {
		result = append(result, *b)
	}
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})

	return result
}

func renderIncidentChart(incidents []api.Incident, width int) string {
	days := width
	if days > 30 {
		days = 30
	}
	if days < 7 {
		days = 7
	}

	buckets := buildDailyBuckets(incidents, days)

	// Find max count for scaling
	maxCount := 1
	for _, b := range buckets {
		if b.Count > maxCount {
			maxCount = b.Count
		}
	}

	// Build chart
	var bars strings.Builder
	var labels strings.Builder

	for i, b := range buckets {
		if b.Count == 0 {
			bars.WriteString(lipgloss.NewStyle().Foreground(colorDarkGray).Render("▁"))
		} else {
			level := (b.Count * (len(barChars) - 1)) / maxCount
			if level >= len(barChars) {
				level = len(barChars) - 1
			}
			c := impactColor(b.Max)
			bars.WriteString(lipgloss.NewStyle().Foreground(c).Render(barChars[level]))
		}

		// Date label every 5 days or first/last
		t, _ := time.Parse("2006-01-02", b.Date)
		if i == 0 || i == len(buckets)-1 || i%5 == 0 {
			day := t.Format("2")
			if len(day) == 1 {
				day = " " + day
			}
			labels.WriteString(day[:1])
		} else {
			labels.WriteString(" ")
		}
	}

	return fmt.Sprintf("  %s\n  %s",
		bars.String(),
		lipgloss.NewStyle().Foreground(colorDim).Render(labels.String()),
	)
}

func renderImpactBreakdown(incidents []api.Incident) string {
	counts := map[string]int{"critical": 0, "major": 0, "minor": 0, "none": 0}
	for _, inc := range incidents {
		counts[inc.Impact]++
	}

	// Find max for bar scaling
	maxCount := 1
	for _, c := range counts {
		if c > maxCount {
			maxCount = c
		}
	}

	header := lipgloss.NewStyle().Foreground(colorDim).Render("last 8 weeks")
	lines := []string{header}

	displayLabels := map[string]string{"critical": "critical", "major": "major", "minor": "minor", "none": "unlabeled"}
	for _, impact := range []string{"critical", "major", "minor", "none"} {
		count := counts[impact]
		dot := impactDot(impact)
		label := lipgloss.NewStyle().Foreground(impactColor(impact)).Width(10).Render(displayLabels[impact])
		countStr := valueStyle.Width(4).Align(lipgloss.Right).Render(fmt.Sprintf("%d", count))

		bar := ""
		if count > 0 {
			barLen := (count * 12) / maxCount
			if barLen < 1 {
				barLen = 1
			}
			bar = lipgloss.NewStyle().Foreground(impactColor(impact)).Render(
				strings.Repeat("━", barLen))
		}

		lines = append(lines, fmt.Sprintf("  %s %s %s %s", dot, label, countStr, bar))
	}

	return strings.Join(lines, "\n")
}

func renderRecentIncidents(incidents []api.Incident, maxItems int) string {
	var lines []string

	count := maxItems
	if count > len(incidents) {
		count = len(incidents)
	}

	for i := 0; i < count; i++ {
		inc := incidents[i]
		var indicator string
		if inc.Status == "resolved" || inc.Status == "postmortem" {
			indicator = resolvedIndicator.Render("✓")
		} else {
			indicator = activeIndicator.Render("●")
		}

		name := inc.Name
		if len(name) > 40 {
			name = name[:37] + "..."
		}

		age := formatAge(inc.CreatedAt)
		nameStyled := lipgloss.NewStyle().Foreground(colorWhite).Render(name)
		ageStyled := lipgloss.NewStyle().Foreground(colorDim).Render(age)
		impactTag := lipgloss.NewStyle().
			Foreground(impactColor(inc.Impact)).
			Render(fmt.Sprintf("[%s]", inc.Impact))

		lines = append(lines, fmt.Sprintf("  %s %s %s", indicator, nameStyled, impactTag))
		lines = append(lines, fmt.Sprintf("    %s", ageStyled))
	}

	return strings.Join(lines, "\n")
}

func renderTimeBuckets(incidents []api.Incident) string {
	now := time.Now()
	windows := []struct {
		label string
		dur   time.Duration
	}{
		{"24h", 24 * time.Hour},
		{"3d", 3 * 24 * time.Hour},
		{"7d", 7 * 24 * time.Hour},
		{"14d", 14 * 24 * time.Hour},
		{"30d", 30 * 24 * time.Hour},
	}

	impacts := []string{"critical", "major", "minor"}

	// Count incidents per window per impact
	type bucketCounts struct {
		total    int
		byImpact map[string]int
	}
	counts := make([]bucketCounts, len(windows))
	for i := range counts {
		counts[i].byImpact = make(map[string]int)
	}
	for _, inc := range incidents {
		age := now.Sub(inc.CreatedAt)
		for i, w := range windows {
			if age <= w.dur {
				counts[i].total++
				counts[i].byImpact[inc.Impact]++
			}
		}
	}

	var lines []string

	// Header row with abbreviated impact labels
	impactLabels := map[string]string{"critical": "crit", "major": "majr", "minor": "minr"}
	headerLabel := lipgloss.NewStyle().Foreground(colorDim).Width(4).Align(lipgloss.Right).Render("")
	headerTotal := lipgloss.NewStyle().Foreground(colorDim).Width(5).Align(lipgloss.Right).Render("total")
	headerParts := []string{headerLabel + " " + headerTotal}
	for _, imp := range impacts {
		headerParts = append(headerParts,
			lipgloss.NewStyle().Foreground(impactColor(imp)).Width(5).Align(lipgloss.Right).Render(impactLabels[imp]))
	}
	lines = append(lines, strings.Join(headerParts, ""))

	// Find max total for bar scaling
	maxCount := 1
	for _, c := range counts {
		if c.total > maxCount {
			maxCount = c.total
		}
	}

	for i, w := range windows {
		bc := counts[i]

		labelStyled := lipgloss.NewStyle().Foreground(colorCyan).Width(4).Align(lipgloss.Right).Render(w.label)
		totalStyled := valueStyle.Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", bc.total))

		parts := []string{labelStyled + " " + totalStyled}
		for _, imp := range impacts {
			c := bc.byImpact[imp]
			styled := lipgloss.NewStyle().Foreground(impactColor(imp)).Width(5).Align(lipgloss.Right).Render(fmt.Sprintf("%d", c))
			parts = append(parts, styled)
		}

		// Stacked mini bar
		barMax := 15
		bar := ""
		if bc.total > 0 {
			for _, imp := range impacts {
				c := bc.byImpact[imp]
				if c == 0 {
					continue
				}
				segLen := (c * barMax) / maxCount
				if segLen < 1 {
					segLen = 1
				}
				bar += lipgloss.NewStyle().Foreground(impactColor(imp)).Render(strings.Repeat("▰", segLen))
			}
			// Fill remainder
			usedLen := 0
			for _, imp := range impacts {
				c := bc.byImpact[imp]
				if c == 0 {
					continue
				}
				segLen := (c * barMax) / maxCount
				if segLen < 1 {
					segLen = 1
				}
				usedLen += segLen
			}
			if usedLen < barMax {
				bar += lipgloss.NewStyle().Foreground(colorDarkGray).Render(strings.Repeat("▱", barMax-usedLen))
			}
		} else {
			bar = lipgloss.NewStyle().Foreground(colorDarkGray).Render(strings.Repeat("▱", barMax))
		}

		parts = append(parts, " "+bar)
		lines = append(lines, strings.Join(parts, ""))
	}

	return strings.Join(lines, "\n")
}

func renderComponents(components []api.Component) string {
	// Header
	dotCol := lipgloss.NewStyle().Width(2).Render("")
	nameCol := lipgloss.NewStyle().Foreground(colorDim).Width(26).Render("Component")
	statusCol := lipgloss.NewStyle().Foreground(colorDim).Width(16).Render("Status")
	header := fmt.Sprintf("%s %s %s", dotCol, nameCol, statusCol)

	lines := []string{header}

	for _, comp := range components {
		if comp.Group || comp.OnlyShowIfDegraded {
			continue
		}

		dot := statusDot(comp.Status)

		compName := comp.Name
		if len(compName) > 26 {
			compName = compName[:23] + "..."
		}
		name := lipgloss.NewStyle().
			Foreground(colorWhite).
			Width(26).
			Render(compName)

		statusLabel := comp.Status
		switch statusLabel {
		case "operational":
			statusLabel = "Operational"
		case "degraded_performance":
			statusLabel = "Degraded"
		case "partial_outage":
			statusLabel = "Partial Outage"
		case "major_outage":
			statusLabel = "Major Outage"
		}

		styledStatus := lipgloss.NewStyle().
			Foreground(statusColor(comp.Status)).
			Width(16).
			Render(statusLabel)

		lines = append(lines, fmt.Sprintf("%s %s %s", dot, name, styledStatus))
	}

	return strings.Join(lines, "\n")
}

func renderRegionOverview(regions []api.Region, incidents []api.Incident) string {
	// Count active incidents per region
	regionCounts := make(map[string]int)
	regionWorst := make(map[string]string) // region code -> worst impact
	impactSeverity := map[string]int{"none": 0, "minor": 1, "major": 2, "critical": 3}

	for _, inc := range incidents {
		if inc.Status == "resolved" || inc.Status == "postmortem" {
			continue
		}
		regionCounts[inc.RegionCode]++
		if impactSeverity[inc.Impact] > impactSeverity[regionWorst[inc.RegionCode]] {
			regionWorst[inc.RegionCode] = inc.Impact
		}
	}

	// Sort: impacted regions first (by severity desc), then alphabetical
	type regionEntry struct {
		Region api.Region
		Active int
		Worst  string
	}
	entries := make([]regionEntry, len(regions))
	for i, r := range regions {
		entries[i] = regionEntry{Region: r, Active: regionCounts[r.Code], Worst: regionWorst[r.Code]}
	}
	sort.SliceStable(entries, func(i, j int) bool {
		si := impactSeverity[entries[i].Worst]
		sj := impactSeverity[entries[j].Worst]
		if si != sj {
			return si > sj
		}
		return entries[i].Region.Code < entries[j].Region.Code
	})

	// Header
	dotCol := lipgloss.NewStyle().Width(2).Render("")
	nameCol := lipgloss.NewStyle().Foreground(colorDim).Width(28).Render("Region")
	statusCol := lipgloss.NewStyle().Foreground(colorDim).Width(16).Render("Status")
	header := fmt.Sprintf("%s %s %s", dotCol, nameCol, statusCol)

	lines := []string{header}

	for _, e := range entries {
		display := e.Region.Name + " (" + e.Region.Code + ")"
		if len(display) > 28 {
			display = display[:25] + "..."
		}

		if e.Active > 0 {
			dot := lipgloss.NewStyle().Foreground(impactColor(e.Worst)).Render("●")
			name := lipgloss.NewStyle().Foreground(colorWhite).Width(28).Render(display)
			label := fmt.Sprintf("%d active", e.Active)
			styledStatus := lipgloss.NewStyle().Foreground(impactColor(e.Worst)).Width(16).Render(label)
			lines = append(lines, fmt.Sprintf("%s %s %s", dot, name, styledStatus))
		} else {
			dot := lipgloss.NewStyle().Foreground(colorGreen).Render("●")
			name := lipgloss.NewStyle().Foreground(colorDim).Width(28).Render(display)
			styledStatus := lipgloss.NewStyle().Foreground(colorGreen).Width(16).Render("Operational")
			lines = append(lines, fmt.Sprintf("%s %s %s", dot, name, styledStatus))
		}
	}

	return strings.Join(lines, "\n")
}

func renderRegionIncidents(incidents []api.Incident) string {
	var lines []string

	for _, inc := range incidents {
		var indicator string
		if inc.Status == "resolved" || inc.Status == "postmortem" {
			indicator = resolvedIndicator.Render("✓")
		} else {
			indicator = activeIndicator.Render("●")
		}

		name := inc.Name
		if len(name) > 44 {
			name = name[:41] + "..."
		}

		age := formatAge(inc.CreatedAt)
		nameStyled := lipgloss.NewStyle().Foreground(colorWhite).Render(name)
		ageStyled := lipgloss.NewStyle().Foreground(colorDim).Render(age)
		impactTag := lipgloss.NewStyle().
			Foreground(impactColor(inc.Impact)).
			Render(fmt.Sprintf("[%s]", inc.Impact))

		lines = append(lines, fmt.Sprintf("  %s %s %s", indicator, nameStyled, impactTag))
		lines = append(lines, fmt.Sprintf("    %s", ageStyled))
	}

	if len(lines) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(colorDim).Render("  No incidents"))
	}

	return strings.Join(lines, "\n")
}

func renderRegionSummary(region api.Region) string {
	header := lipgloss.NewStyle().Bold(true).Foreground(colorCyan).
		Render(region.Name + " (" + region.Code + ")")

	checkmark := lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("✓")
	status := lipgloss.NewStyle().Foreground(colorGreen).Bold(true).
		Render("All Systems Operational")

	detail := lipgloss.NewStyle().Foreground(colorDim).
		Render("No incidents reported in this region\nfor the last 8 weeks.")

	hint := lipgloss.NewStyle().Foreground(colorDim).Italic(true).
		Render("Press [ ] to switch regions")

	return strings.Join([]string{
		header,
		"",
		checkmark + " " + status,
		"",
		detail,
		"",
		hint,
	}, "\n")
}

func formatAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
}
