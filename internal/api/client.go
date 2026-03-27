package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Client struct {
	http    *http.Client
	baseURL string
}

func NewClient(baseURL string) *Client {
	return &Client{
		http:    &http.Client{Timeout: 10 * time.Second},
		baseURL: baseURL,
	}
}

func (c *Client) FetchSummary() (*SummaryResponse, error) {
	resp, err := c.http.Get(c.baseURL + "/summary.json")
	if err != nil {
		return nil, fmt.Errorf("fetching summary: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("summary returned status %d", resp.StatusCode)
	}

	var summary SummaryResponse
	if err := json.NewDecoder(resp.Body).Decode(&summary); err != nil {
		return nil, fmt.Errorf("decoding summary: %w", err)
	}
	return &summary, nil
}

func (c *Client) FetchIncidents() (*IncidentsResponse, error) {
	cutoff := time.Now().AddDate(0, 0, -56) // 8 weeks for trend analysis

	resp, err := c.http.Get(c.baseURL + "/incidents.json")
	if err != nil {
		return nil, fmt.Errorf("fetching incidents: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("incidents returned status %d", resp.StatusCode)
	}

	var raw IncidentsResponse
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("decoding incidents: %w", err)
	}

	// Filter to cutoff window and deduplicate by ID
	seen := make(map[string]bool)
	var filtered []Incident
	for _, inc := range raw.Incidents {
		if inc.CreatedAt.Before(cutoff) {
			continue
		}
		if seen[inc.ID] {
			continue
		}
		seen[inc.ID] = true
		filtered = append(filtered, inc)
	}

	return &IncidentsResponse{Incidents: filtered}, nil
}

type DashboardData struct {
	Summary   *SummaryResponse
	Incidents *IncidentsResponse
	FetchedAt time.Time
}

func (c *Client) FetchAll() (*DashboardData, error) {
	summary, err := c.FetchSummary()
	if err != nil {
		return nil, err
	}

	incidents, err := c.FetchIncidents()
	if err != nil {
		return nil, err
	}

	return &DashboardData{
		Summary:   summary,
		Incidents: incidents,
		FetchedAt: time.Now(),
	}, nil
}
