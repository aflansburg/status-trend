package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// Google Cloud status page types
// Dedicatd implementation due to GCP status complexity

type gcIncident struct {
	ID               string `json:"id"`
	Begin            string `json:"begin"`
	End              string `json:"end"`
	Created          string `json:"created"`
	Modified         string `json:"modified"`
	ExternalDesc     string `json:"external_desc"`
	StatusImpact     string `json:"status_impact"`
	Severity         string `json:"severity"`
	ServiceName      string `json:"service_name"`
	URI              string `json:"uri"`
	AffectedProducts []struct {
		Title string `json:"title"`
		ID    string `json:"id"`
	} `json:"affected_products"`
	Updates []struct {
		Created string `json:"created"`
		Text    string `json:"text"`
		Status  string `json:"status"`
	} `json:"updates"`
}

// AI/ML product keywords to filter on
var aiProductKeywords = []string{
	"vertex ai", "gemini", "ai platform", "dialogflow",
	"agent", "natural language", "vision ai", "speech",
	"translation", "document ai", "cloud tpu",
	"machine learning", "automl", "generative ai",
}

func isAIRelated(inc gcIncident) bool {
	check := strings.ToLower(inc.ExternalDesc + " " + inc.ServiceName)
	for _, p := range inc.AffectedProducts {
		check += " " + strings.ToLower(p.Title)
	}
	for _, kw := range aiProductKeywords {
		if strings.Contains(check, kw) {
			return true
		}
	}
	return false
}

type GoogleCloudClient struct {
	http *http.Client
}

func NewGoogleCloudClient() *GoogleCloudClient {
	return &GoogleCloudClient{
		http: &http.Client{Timeout: 10 * time.Second},
	}
}

func (g *GoogleCloudClient) FetchAll() (*DashboardData, error) {
	resp, err := g.http.Get("https://status.cloud.google.com/incidents.json")
	if err != nil {
		return nil, fmt.Errorf("fetching google cloud incidents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("google cloud incidents returned status %d", resp.StatusCode)
	}

	var gcIncidents []gcIncident
	if err := json.NewDecoder(resp.Body).Decode(&gcIncidents); err != nil {
		return nil, fmt.Errorf("decoding google cloud incidents: %w", err)
	}

	// Filter to AI-related incidents and convert to our types
	var incidents []Incident
	productSet := make(map[string]bool)

	for _, gc := range gcIncidents {
		if !isAIRelated(gc) {
			continue
		}

		// Track affected AI products as pseudo-components
		for _, p := range gc.AffectedProducts {
			lower := strings.ToLower(p.Title)
			for _, kw := range aiProductKeywords {
				if strings.Contains(lower, kw) {
					productSet[p.Title] = true
					break
				}
			}
		}

		inc := convertGCIncident(gc)
		incidents = append(incidents, inc)
	}

	// Build pseudo-components from seen AI products
	var components []Component
	i := 0
	for name := range productSet {
		components = append(components, Component{
			ID:       fmt.Sprintf("gc-%d", i),
			Name:     name,
			Status:   "operational", // no live status from Google
			Position: i,
			Showcase: true,
		})
		i++
	}

	// If no AI products found, add generic
	if len(components) == 0 {
		components = []Component{
			{ID: "gc-0", Name: "Vertex AI / Gemini", Status: "operational", Showcase: true},
			{ID: "gc-1", Name: "Dialogflow", Status: "operational", Showcase: true},
			{ID: "gc-2", Name: "Cloud TPU", Status: "operational", Showcase: true},
			{ID: "gc-3", Name: "Natural Language AI", Status: "operational", Showcase: true},
		}
	}

	// Determine overall status
	status := StatusIndicator{Indicator: "none", Description: "All Systems Operational"}
	if len(incidents) > 0 {
		latest := incidents[0]
		if latest.Status != "resolved" && latest.Status != "postmortem" {
			status.Indicator = "major"
			status.Description = "Active Incident"
		}
	}

	return &DashboardData{
		Summary: &SummaryResponse{
			Page: Page{
				Name: "Google Cloud AI",
				URL:  "https://status.cloud.google.com",
			},
			Components: components,
			Status:     status,
		},
		Incidents: &IncidentsResponse{
			Incidents: incidents,
		},
		FetchedAt: time.Now(),
	}, nil
}

func convertGCIncident(gc gcIncident) Incident {
	createdAt := parseGCTime(gc.Created)
	beginAt := parseGCTime(gc.Begin)

	var resolvedAt *time.Time
	status := "investigating"
	if gc.End != "" {
		t := parseGCTime(gc.End)
		resolvedAt = &t
		status = "resolved"
	}

	// Map Google severity to impact
	impact := mapGCSeverity(gc.Severity, gc.StatusImpact)

	// Build truncated name from description
	name := gc.ExternalDesc
	if len(name) > 80 {
		name = name[:77] + "..."
	}

	// Convert updates
	var updates []IncidentUpdate
	for _, u := range gc.Updates {
		updates = append(updates, IncidentUpdate{
			Status:    mapGCUpdateStatus(u.Status),
			Body:      u.Text,
			CreatedAt: parseGCTime(u.Created),
			UpdatedAt: parseGCTime(u.Created),
		})
	}

	return Incident{
		ID:              gc.ID,
		Name:            name,
		Status:          status,
		CreatedAt:       createdAt,
		UpdatedAt:       createdAt,
		StartedAt:       beginAt,
		ResolvedAt:      resolvedAt,
		Impact:          impact,
		Shortlink:       "https://status.cloud.google.com/" + gc.URI,
		IncidentUpdates: updates,
	}
}

func mapGCSeverity(severity, statusImpact string) string {
	// status_impact: SERVICE_INFORMATION, SERVICE_DISRUPTION, SERVICE_OUTAGE
	// severity: low, medium, high
	switch statusImpact {
	case "SERVICE_OUTAGE":
		return "critical"
	case "SERVICE_DISRUPTION":
		if severity == "high" {
			return "critical"
		}
		return "major"
	case "SERVICE_INFORMATION":
		return "minor"
	default:
		switch severity {
		case "high":
			return "critical"
		case "medium":
			return "major"
		default:
			return "minor"
		}
	}
}

func mapGCUpdateStatus(status string) string {
	switch status {
	case "AVAILABLE":
		return "resolved"
	case "SERVICE_DISRUPTION", "SERVICE_OUTAGE":
		return "investigating"
	default:
		return "investigating"
	}
}

func parseGCTime(s string) time.Time {
	// Google uses ISO 8601 with timezone offset
	formats := []string{
		time.RFC3339,
		"2006-01-02T15:04:05-07:00",
		"2006-01-02T15:04:05Z",
	}
	for _, f := range formats {
		if t, err := time.Parse(f, s); err == nil {
			return t
		}
	}
	return time.Time{}
}
