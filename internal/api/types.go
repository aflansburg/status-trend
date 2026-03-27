package api

import "time"

type SummaryResponse struct {
	Page                  Page                `json:"page"`
	Components            []Component         `json:"components"`
	Incidents             []Incident          `json:"incidents"`
	ScheduledMaintenances []Incident          `json:"scheduled_maintenances"`
	Status                StatusIndicator     `json:"status"`
}

type IncidentsResponse struct {
	Page      Page       `json:"page"`
	Incidents []Incident `json:"incidents"`
}

type Page struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	TimeZone  string    `json:"time_zone"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Component struct {
	ID                 string    `json:"id"`
	Name               string    `json:"name"`
	Status             string    `json:"status"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
	Position           int       `json:"position"`
	Description        *string   `json:"description"`
	Showcase           bool      `json:"showcase"`
	GroupID            *string   `json:"group_id"`
	PageID             string    `json:"page_id"`
	Group              bool      `json:"group"`
	OnlyShowIfDegraded bool      `json:"only_show_if_degraded"`
}

type Incident struct {
	ID               string           `json:"id"`
	Name             string           `json:"name"`
	Status           string           `json:"status"`
	CreatedAt        time.Time        `json:"created_at"`
	UpdatedAt        time.Time        `json:"updated_at"`
	MonitoringAt     *time.Time       `json:"monitoring_at"`
	ResolvedAt       *time.Time       `json:"resolved_at"`
	Impact           string           `json:"impact"`
	Shortlink        string           `json:"shortlink"`
	StartedAt        time.Time        `json:"started_at"`
	PageID           string           `json:"page_id"`
	IncidentUpdates  []IncidentUpdate `json:"incident_updates"`
	Components       []Component      `json:"components"`
}

type IncidentUpdate struct {
	ID                   string              `json:"id"`
	Status               string              `json:"status"`
	Body                 string              `json:"body"`
	IncidentID           string              `json:"incident_id"`
	CreatedAt            time.Time           `json:"created_at"`
	UpdatedAt            time.Time           `json:"updated_at"`
	DisplayAt            time.Time           `json:"display_at"`
	AffectedComponents   []AffectedComponent `json:"affected_components"`
	DeliverNotifications bool                `json:"deliver_notifications"`
}

type AffectedComponent struct {
	Code      string `json:"code"`
	Name      string `json:"name"`
	OldStatus string `json:"old_status"`
	NewStatus string `json:"new_status"`
}

type StatusIndicator struct {
	Indicator   string `json:"indicator"`
	Description string `json:"description"`
}
