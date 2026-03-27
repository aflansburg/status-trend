package ui

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"status-trend/internal/api"
)

var sparkChars = []string{"▁", "▂", "▃", "▄", "▅", "▆", "▇", "█"}

func renderTrends(incidents []api.Incident) string {
	var sections []string

	sections = append(sections, renderReliabilitySparkline(incidents))
	sections = append(sections, renderMTTR(incidents))
	sections = append(sections, renderWeekOverWeek(incidents))
	sections = append(sections, renderSeverityShift(incidents))

	return strings.Join(sections, "\n\n")
}

// renderReliabilitySparkline shows a rolling 7-day incident count sparkline
func renderReliabilitySparkline(incidents []api.Incident) string {
	now := time.Now().UTC()
	weeks := 8 // 8 data points, each a 7-day window

	counts := make([]int, weeks)
	for _, inc := range incidents {
		age := now.Sub(inc.CreatedAt)
		weekIdx := int(age.Hours() / (24 * 7))
		if weekIdx >= 0 && weekIdx < weeks {
			counts[weekIdx]++
		}
	}

	// Reverse so oldest is left, newest is right
	for i, j := 0, len(counts)-1; i < j; i, j = i+1, j-1 {
		counts[i], counts[j] = counts[j], counts[i]
	}

	// Build sparkline
	maxVal := 1
	for _, c := range counts {
		if c > maxVal {
			maxVal = c
		}
	}

	var spark strings.Builder
	for _, c := range counts {
		level := 0
		if c > 0 {
			level = (c * (len(sparkChars) - 1)) / maxVal
		}
		var clr = colorGreen
		switch {
		case c == 0:
			clr = colorGreen
		case c <= 2:
			clr = colorYellow
		case c <= 5:
			clr = colorOrange
		default:
			clr = colorRed
		}
		spark.WriteString(lipgloss.NewStyle().Foreground(clr).Render(sparkChars[level]))
	}

	// Trend arrow based on last 2 weeks vs prior 2 weeks
	recent := counts[weeks-1] + counts[weeks-2]
	prior := counts[weeks-3] + counts[weeks-4]

	var arrow string
	if recent > prior {
		arrow = lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(" ↓ worsening")
	} else if recent < prior {
		arrow = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render(" ↑ improving")
	} else {
		arrow = lipgloss.NewStyle().Foreground(colorDim).Render(" → stable")
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render("Reliability (8 weeks)")
	labels := lipgloss.NewStyle().Foreground(colorDim).Render("old              new")

	return header + "\n" + spark.String() + arrow + "\n" + labels
}

// renderMTTR shows mean time to resolve per recent week
func renderMTTR(incidents []api.Incident) string {
	now := time.Now().UTC()

	type weekData struct {
		totalDur time.Duration
		count    int
	}

	weeks := make([]weekData, 4) // last 4 weeks

	for _, inc := range incidents {
		if inc.ResolvedAt == nil {
			continue
		}
		dur := inc.ResolvedAt.Sub(inc.CreatedAt)
		if dur < 0 {
			continue
		}
		age := now.Sub(inc.CreatedAt)
		weekIdx := int(age.Hours() / (24 * 7))
		if weekIdx >= 0 && weekIdx < 4 {
			weeks[weekIdx].totalDur += dur
			weeks[weekIdx].count++
		}
	}

	// Reverse so oldest left, newest right
	for i, j := 0, len(weeks)-1; i < j; i, j = i+1, j-1 {
		weeks[i], weeks[j] = weeks[j], weeks[i]
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render("MTTR (last 4 weeks)")

	var lines []string
	labels := []string{"4w ago", "3w ago", "2w ago", "1w ago"}
	maxMTTR := 1.0

	mttrs := make([]float64, 4)
	for i, w := range weeks {
		if w.count > 0 {
			mttrs[i] = w.totalDur.Hours() / float64(w.count)
		}
		if mttrs[i] > maxMTTR {
			maxMTTR = mttrs[i]
		}
	}

	for i, mttr := range mttrs {
		label := lipgloss.NewStyle().Foreground(colorDim).Width(7).Render(labels[i])
		if weeks[i].count == 0 {
			lines = append(lines, label+" "+lipgloss.NewStyle().Foreground(colorDim).Render("  ─"))
			continue
		}

		barLen := int(math.Ceil(mttr / maxMTTR * 10))
		if barLen < 1 {
			barLen = 1
		}

		var clr = colorGreen
		switch {
		case mttr > 6:
			clr = colorRed
		case mttr > 2:
			clr = colorOrange
		case mttr > 0.5:
			clr = colorYellow
		}

		bar := lipgloss.NewStyle().Foreground(clr).Render(strings.Repeat("━", barLen))
		timeStr := formatDuration(time.Duration(mttr * float64(time.Hour)))
		timeStyled := lipgloss.NewStyle().Foreground(colorWhite).Render(timeStr)

		lines = append(lines, label+" "+bar+" "+timeStyled)
	}

	return header + "\n" + strings.Join(lines, "\n")
}

// renderWeekOverWeek shows this week vs last week delta
func renderWeekOverWeek(incidents []api.Incident) string {
	now := time.Now().UTC()
	thisWeek := 0
	lastWeek := 0
	sevenDays := 7 * 24 * time.Hour

	for _, inc := range incidents {
		age := now.Sub(inc.CreatedAt)
		if age <= sevenDays {
			thisWeek++
		} else if age <= 2*sevenDays {
			lastWeek++
		}
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render("Week over Week")

	thisStr := valueStyle.Render(fmt.Sprintf("%d", thisWeek))
	lastStr := lipgloss.NewStyle().Foreground(colorDim).Render(fmt.Sprintf("%d", lastWeek))

	delta := thisWeek - lastWeek
	var deltaStr string
	if delta > 0 {
		deltaStr = lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render(fmt.Sprintf("▲ +%d", delta))
	} else if delta < 0 {
		deltaStr = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render(fmt.Sprintf("▼ %d", delta))
	} else {
		deltaStr = lipgloss.NewStyle().Foreground(colorDim).Render("─ 0")
	}

	line1 := fmt.Sprintf("This week: %s  Last week: %s", thisStr, lastStr)
	line2 := fmt.Sprintf("Change: %s", deltaStr)

	return header + "\n" + line1 + "\n" + line2
}

// renderSeverityShift shows ratio of critical+major vs minor trending
func renderSeverityShift(incidents []api.Incident) string {
	now := time.Now().UTC()
	twoWeeks := 14 * 24 * time.Hour

	type sevBucket struct {
		high int // critical + major
		low  int // minor + none
	}

	var recent, prior sevBucket

	for _, inc := range incidents {
		age := now.Sub(inc.CreatedAt)
		isSevere := inc.Impact == "critical" || inc.Impact == "major"
		if age <= twoWeeks {
			if isSevere {
				recent.high++
			} else {
				recent.low++
			}
		} else if age <= 2*twoWeeks {
			if isSevere {
				prior.high++
			} else {
				prior.low++
			}
		}
	}

	header := lipgloss.NewStyle().Bold(true).Foreground(colorWhite).Render("Severity Trend (2-week)")

	recentTotal := recent.high + recent.low
	priorTotal := prior.high + prior.low

	recentPct := 0.0
	if recentTotal > 0 {
		recentPct = float64(recent.high) / float64(recentTotal) * 100
	}
	priorPct := 0.0
	if priorTotal > 0 {
		priorPct = float64(prior.high) / float64(priorTotal) * 100
	}

	// Visual severity bar for recent
	barWidth := 20
	severeLen := 0
	mildLen := barWidth
	if recentTotal > 0 {
		severeLen = int(math.Round(float64(recent.high) / float64(recentTotal) * float64(barWidth)))
		mildLen = barWidth - severeLen
	}
	bar := lipgloss.NewStyle().Foreground(colorRed).Render(strings.Repeat("▰", severeLen)) +
		lipgloss.NewStyle().Foreground(colorYellow).Render(strings.Repeat("▰", mildLen))

	pctStr := lipgloss.NewStyle().Foreground(colorWhite).Render(fmt.Sprintf("%.0f", recentPct))
	recentLine := "Recent:  " + bar + " " + pctStr + "% severe"

	var trend string
	if recentPct > priorPct+10 {
		trend = lipgloss.NewStyle().Foreground(colorRed).Bold(true).Render("▲ more severe")
	} else if recentPct < priorPct-10 {
		trend = lipgloss.NewStyle().Foreground(colorGreen).Bold(true).Render("▼ less severe")
	} else {
		trend = lipgloss.NewStyle().Foreground(colorDim).Render("→ stable mix")
	}

	priorPctStr := fmt.Sprintf("%.0f", priorPct)
	priorLine := "Prior:   " + priorPctStr + "% severe  " + trend

	return header + "\n" + recentLine + "\n" + priorLine
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	hours := d.Hours()
	if hours < 24 {
		return fmt.Sprintf("%.1fh", hours)
	}
	return fmt.Sprintf("%.1fd", hours/24)
}

// sortIncidentsByDate returns a copy sorted newest first
func sortIncidentsByDate(incidents []api.Incident) []api.Incident {
	sorted := make([]api.Incident, len(incidents))
	copy(sorted, incidents)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].CreatedAt.After(sorted[j].CreatedAt)
	})
	return sorted
}
