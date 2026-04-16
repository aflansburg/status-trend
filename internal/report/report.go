package report

import (
	"fmt"
	"io"
	"math"
	"sort"
	"strings"
	"sync"
	"time"

	"status-trend/internal/api"
)

type vendorResult struct {
	name string
	data *api.DashboardData
	err  error
}

// WriteAll fetches data from all vendors and writes an LLM-friendly report to w.
func WriteAll(w io.Writer) error {
	vendors := api.Vendors
	results := make([]vendorResult, len(vendors))

	var wg sync.WaitGroup
	for i, v := range vendors {
		wg.Add(1)
		go func(idx int, vendor api.Vendor) {
			defer wg.Done()
			f := vendor.NewFetcher()
			data, err := f.FetchAll()
			results[idx] = vendorResult{name: vendor.Name, data: data, err: err}
		}(i, v)
	}
	wg.Wait()

	fmt.Fprintf(w, "# Status Report\n")
	fmt.Fprintf(w, "Generated: %s\n\n", time.Now().UTC().Format(time.RFC3339))

	for _, r := range results {
		writeVendor(w, r)
		fmt.Fprintln(w)
	}

	return nil
}

// validStart returns a usable start time for an incident, falling back to
// CreatedAt when StartedAt is missing/zero, and returning zero if neither is valid.
func validStart(inc api.Incident) time.Time {
	if !inc.StartedAt.IsZero() && inc.StartedAt.Year() >= 2000 {
		return inc.StartedAt
	}
	if !inc.CreatedAt.IsZero() && inc.CreatedAt.Year() >= 2000 {
		return inc.CreatedAt
	}
	return time.Time{}
}

func writeVendor(w io.Writer, r vendorResult) {
	fmt.Fprintf(w, "## %s\n", r.name)

	if r.err != nil {
		fmt.Fprintf(w, "Error: %s\n", r.err)
		return
	}

	d := r.data
	fmt.Fprintf(w, "Fetched: %s\n", d.FetchedAt.UTC().Format(time.RFC3339))

	if d.Summary != nil {
		fmt.Fprintf(w, "Overall: %s\n", d.Summary.Status.Description)
	}

	// Component uptime chart (replaces simple component listing)
	writeUptimeChart(w, d)

	// Components not currently operational
	if d.Summary != nil {
		var degraded []api.Component
		for _, c := range d.Summary.Components {
			if c.Group || c.Status == "operational" {
				continue
			}
			degraded = append(degraded, c)
		}
		if len(degraded) > 0 {
			fmt.Fprintln(w, "\nCurrently Degraded:")
			for _, c := range degraded {
				fmt.Fprintf(w, "- %s: %s\n", c.Name, formatStatus(c.Status))
			}
		}
	}

	// Active incidents
	if d.Summary != nil {
		var active []api.Incident
		for _, inc := range d.Summary.Incidents {
			if inc.Status != "resolved" && inc.Status != "postmortem" {
				active = append(active, inc)
			}
		}
		for _, inc := range d.Summary.ScheduledMaintenances {
			if inc.Status != "completed" {
				active = append(active, inc)
			}
		}

		fmt.Fprintf(w, "\nActive Incidents: %d\n", len(active))
		for _, inc := range active {
			start := validStart(inc)
			since := "unknown"
			if !start.IsZero() {
				since = start.UTC().Format("2006-01-02 15:04 UTC")
			}
			fmt.Fprintf(w, "- [%s] %s (status: %s, since: %s)\n",
				inc.Impact, inc.Name, inc.Status, since)
			if len(inc.IncidentUpdates) > 0 {
				latest := inc.IncidentUpdates[0]
				fmt.Fprintf(w, "  Latest update (%s): %s\n",
					latest.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"),
					truncate(latest.Body, 200))
			}
		}
	}

	// Incident density chart
	if d.Incidents != nil {
		writeIncidentDensity(w, d)
	}

	// Incident history summary + recent list
	if d.Incidents != nil && len(d.Incidents.Incidents) > 0 {
		incidents := make([]api.Incident, len(d.Incidents.Incidents))
		copy(incidents, d.Incidents.Incidents)

		sort.Slice(incidents, func(i, j int) bool {
			return incidents[i].CreatedAt.After(incidents[j].CreatedAt)
		})

		counts := map[string]int{}
		for _, inc := range incidents {
			counts[inc.Impact]++
		}

		fmt.Fprintf(w, "\nIncident History (8 weeks): %d total", len(incidents))
		for _, sev := range []string{"critical", "major", "minor", "none"} {
			if c := counts[sev]; c > 0 {
				fmt.Fprintf(w, ", %d %s", c, sev)
			}
		}
		fmt.Fprintln(w)

		// MTTR
		var totalResolve time.Duration
		var resolvedCount int
		for _, inc := range incidents {
			if inc.ResolvedAt != nil {
				start := validStart(inc)
				if start.IsZero() {
					continue
				}
				dur := inc.ResolvedAt.Sub(start)
				if dur > 0 {
					totalResolve += dur
					resolvedCount++
				}
			}
		}
		if resolvedCount > 0 {
			mttr := totalResolve / time.Duration(resolvedCount)
			fmt.Fprintf(w, "MTTR: %s (based on %d resolved incidents)\n", formatDuration(mttr), resolvedCount)
		}

		// Recent incidents (up to 10)
		limit := 10
		if len(incidents) < limit {
			limit = len(incidents)
		}
		fmt.Fprintln(w, "\nRecent Incidents:")
		for _, inc := range incidents[:limit] {
			resolved := "unresolved"
			if inc.ResolvedAt != nil {
				resolved = fmt.Sprintf("resolved %s", inc.ResolvedAt.UTC().Format("2006-01-02 15:04 UTC"))
			}
			started := "unknown"
			if s := validStart(inc); !s.IsZero() {
				started = s.UTC().Format("2006-01-02 15:04 UTC")
			}
			fmt.Fprintf(w, "- [%s] %s\n  Started: %s | %s\n",
				inc.Impact, inc.Name, started, resolved)
		}
	} else {
		fmt.Fprintln(w, "\nIncident History (8 weeks): 0 incidents")
	}
}

// ── Component Uptime Chart ──────────────────────────────────────────────

const uptimeBarWidth = 20

type timeInterval struct {
	start, end time.Time
}

func writeUptimeChart(w io.Writer, data *api.DashboardData) {
	if data.Summary == nil || len(data.Summary.Components) == 0 || data.Incidents == nil {
		return
	}

	now := time.Now()
	periodStart := now.AddDate(0, 0, -56)
	totalHours := now.Sub(periodStart).Hours()

	// Collect per-component downtime intervals from incidents
	compIntervals := map[string][]timeInterval{} // component ID -> intervals

	for _, inc := range data.Incidents.Incidents {
		start := validStart(inc)
		if start.IsZero() {
			continue
		}
		end := now
		if inc.ResolvedAt != nil {
			end = *inc.ResolvedAt
		}
		if !end.After(start) {
			continue
		}
		if start.Before(periodStart) {
			start = periodStart
		}

		iv := timeInterval{start: start, end: end}

		if len(inc.Components) > 0 {
			for _, comp := range inc.Components {
				compIntervals[comp.ID] = append(compIntervals[comp.ID], iv)
			}
		} else {
			// No per-component data — attribute to all
			for _, comp := range data.Summary.Components {
				if !comp.Group {
					compIntervals[comp.ID] = append(compIntervals[comp.ID], iv)
				}
			}
		}
	}

	type compRow struct {
		name          string
		uptimePct     float64
		degradedHours float64
	}

	var rows []compRow
	maxName := 0

	for _, comp := range data.Summary.Components {
		if comp.Group {
			continue
		}
		downtime := mergedDuration(compIntervals[comp.ID])
		dh := downtime.Hours()
		pct := 100.0 * (1.0 - dh/totalHours)
		if pct > 100 {
			pct = 100
		}
		if pct < 0 {
			pct = 0
		}

		name := comp.Name
		if len(name) > 28 {
			name = name[:25] + "..."
		}
		if len(name) > maxName {
			maxName = len(name)
		}
		rows = append(rows, compRow{name: name, uptimePct: pct, degradedHours: dh})
	}

	if len(rows) == 0 {
		return
	}

	// Determine scale floor
	minPct := 100.0
	for _, r := range rows {
		if r.uptimePct < minPct {
			minPct = r.uptimePct
		}
	}
	floor := math.Floor(minPct)
	if floor > 97 {
		floor = 97
	}
	if floor < 0 {
		floor = 0
	}
	pctRange := 100.0 - floor

	headerWidth := maxName + 10 + uptimeBarWidth + 5
	fmt.Fprintf(w, "\nComponent Uptime (8 weeks)\n")
	fmt.Fprintf(w, "%s\n\n", strings.Repeat("═", headerWidth))

	for _, r := range rows {
		filled := int(math.Round(float64(uptimeBarWidth) * (r.uptimePct - floor) / pctRange))
		if filled < 0 {
			filled = 0
		}
		if filled > uptimeBarWidth {
			filled = uptimeBarWidth
		}
		empty := uptimeBarWidth - filled

		bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

		var annotation string
		if r.uptimePct >= 99.9 && r.degradedHours < 1 {
			annotation = " ✅"
		} else {
			if r.degradedHours >= 1 {
				annotation = fmt.Sprintf(" ~%s degraded", formatHours(r.degradedHours))
			}
			if r.uptimePct < 99.0 {
				annotation = " ⚠️" + annotation
			}
		}

		fmt.Fprintf(w, "%-*s  %6.2f%% %s%s\n", maxName, r.name, r.uptimePct, bar, annotation)
	}

	// Axis line
	pad := strings.Repeat(" ", maxName+9)
	seg := uptimeBarWidth/4 - 1
	axis := "├" + strings.Repeat("─", seg) + "┼" +
		strings.Repeat("─", seg) + "┼" +
		strings.Repeat("─", seg) + "┼" +
		strings.Repeat("─", seg) + "┤"
	fmt.Fprintf(w, "\n%s%s\n", pad, axis)

	// Axis labels
	step := pctRange / 4
	marks := make([]string, 5)
	for i := range 5 {
		v := floor + float64(i)*step
		if v == math.Floor(v) {
			marks[i] = fmt.Sprintf("%.0f%%", v)
		} else {
			marks[i] = fmt.Sprintf("%.1f%%", v)
		}
	}
	segW := (uptimeBarWidth + 1) / 4
	axisLabels := marks[0]
	for i := 1; i < 5; i++ {
		gap := segW - len(marks[i-1])
		if gap < 1 {
			gap = 1
		}
		axisLabels += strings.Repeat(" ", gap) + marks[i]
	}
	fmt.Fprintf(w, "%s%s\n", pad, axisLabels)
}

func mergedDuration(intervals []timeInterval) time.Duration {
	if len(intervals) == 0 {
		return 0
	}
	sort.Slice(intervals, func(i, j int) bool {
		return intervals[i].start.Before(intervals[j].start)
	})
	merged := []timeInterval{intervals[0]}
	for _, iv := range intervals[1:] {
		last := &merged[len(merged)-1]
		if !iv.start.After(last.end) {
			if iv.end.After(last.end) {
				last.end = iv.end
			}
		} else {
			merged = append(merged, iv)
		}
	}
	var total time.Duration
	for _, iv := range merged {
		total += iv.end.Sub(iv.start)
	}
	return total
}

// ── Incident Density Chart ──────────────────────────────────────────────

func writeIncidentDensity(w io.Writer, data *api.DashboardData) {
	if data.Incidents == nil || len(data.Incidents.Incidents) == 0 {
		return
	}

	now := time.Now()
	const days = 10
	const densityBarWidth = 5

	type dayBucket struct {
		date   time.Time
		count  int
		labels []string
		active bool // has unresolved incident
	}

	buckets := make([]dayBucket, days)
	for i := range days {
		d := now.AddDate(0, 0, -(days - 1 - i))
		buckets[i] = dayBucket{date: d}
	}

	startDay := time.Date(
		buckets[0].date.Year(), buckets[0].date.Month(), buckets[0].date.Day(),
		0, 0, 0, 0, now.Location(),
	)

	for _, inc := range data.Incidents.Incidents {
		t := validStart(inc)
		if t.IsZero() {
			continue
		}
		dayIdx := int(t.Sub(startDay).Hours() / 24)
		if dayIdx < 0 || dayIdx >= days {
			continue
		}
		buckets[dayIdx].count++
		buckets[dayIdx].labels = append(buckets[dayIdx].labels, shortLabel(inc.Name))
		if inc.ResolvedAt == nil {
			buckets[dayIdx].active = true
		}
	}

	maxCount := 0
	for _, b := range buckets {
		if b.count > maxCount {
			maxCount = b.count
		}
	}
	if maxCount == 0 {
		return
	}

	fmt.Fprintf(w, "\nIncident Density — Last %d Days (%s – %s)\n",
		days,
		buckets[0].date.Format("Jan 2"),
		buckets[days-1].date.Format("Jan 2"))
	fmt.Fprintf(w, "%s\n\n", strings.Repeat("═", 55))

	for _, b := range buckets {
		filled := 0
		if b.count > 0 {
			filled = int(math.Ceil(float64(b.count) / float64(maxCount) * float64(densityBarWidth)))
			if filled < 1 {
				filled = 1
			}
		}
		empty := densityBarWidth - filled
		bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)

		var desc string
		switch {
		case b.count == 0:
			desc = "(quiet)"
		default:
			desc = strings.Join(b.labels, " + ")
			if len(desc) > 55 {
				desc = desc[:52] + "..."
			}
			if b.count > 2 {
				desc += fmt.Sprintf(" (%d incidents)", b.count)
			}
			if b.active {
				desc += " ← ACTIVE"
			}
		}

		fmt.Fprintf(w, "%s  %s  %s\n", b.date.Format("Jan _2"), bar, desc)
	}
}

// shortLabel extracts a short identifier from an incident name.
func shortLabel(name string) string {
	for _, prefix := range []string{
		"Elevated errors on ", "Elevated errors with ", "Elevated error rates for ",
		"Elevated Error Rates", "Elevated Errors with ",
		"Incident with ", "Disruption with ", "Issues with ",
		"Degraded Performance on ", "Degraded service on ",
		"Some users may experience issues ",
		"Some users may experience ",
	} {
		if strings.HasPrefix(name, prefix) {
			name = name[len(prefix):]
			break
		}
	}
	if len(name) > 30 {
		name = name[:27] + "..."
	}
	return name
}

// ── Helpers ─────────────────────────────────────────────────────────────

func formatStatus(s string) string {
	switch s {
	case "operational":
		return "operational"
	case "degraded_performance":
		return "degraded"
	case "partial_outage":
		return "partial outage"
	case "major_outage":
		return "major outage"
	default:
		return s
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h >= 24 {
		days := h / 24
		h = h % 24
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func formatHours(h float64) string {
	if h < 1 {
		return fmt.Sprintf("%.0fm", h*60)
	}
	if h >= 24 {
		d := int(h / 24)
		rem := int(h) % 24
		if rem > 0 {
			return fmt.Sprintf("%dd %dh", d, rem)
		}
		return fmt.Sprintf("%dd", d)
	}
	return fmt.Sprintf("%.0fh", h)
}

func truncate(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
