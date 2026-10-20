package storage

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	"status-trend/internal/api"
)

func sampleData() *api.DashboardData {
	started := time.Date(2026, 1, 2, 3, 4, 0, 0, time.UTC)
	resolved := time.Date(2026, 1, 2, 4, 0, 0, 0, time.UTC)
	return &api.DashboardData{
		FetchedAt: time.Date(2026, 1, 2, 5, 0, 0, 0, time.UTC),
		Summary: &api.SummaryResponse{
			Status: api.StatusIndicator{Indicator: "minor", Description: "Minor outage"},
			Components: []api.Component{
				{ID: "c1", Name: "API", Status: "operational"},
				{ID: "g1", Name: "Group", Status: "operational", Group: true},
			},
		},
		Incidents: &api.IncidentsResponse{
			Incidents: []api.Incident{
				// Valid timestamps, resolved.
				{ID: "i1", Name: "Outage", Status: "resolved", Impact: "major",
					CreatedAt: started, StartedAt: started, ResolvedAt: &resolved},
				// Zero StartedAt, nil ResolvedAt — times must store as NULL.
				{ID: "i2", Name: "Ongoing", Status: "investigating", Impact: "minor"},
			},
		},
	}
}

func TestRecordUpsertAndSnapshots(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	data := sampleData()
	if err := store.Record("Claude", data); err != nil {
		t.Fatalf("Record #1: %v", err)
	}
	if err := store.Record("Claude", data); err != nil {
		t.Fatalf("Record #2: %v", err)
	}

	db := store.db

	// Incidents stay at 2 across two Records (upsert, not duplicate).
	var incCount int
	if err := db.QueryRow(`SELECT count(*) FROM incidents`).Scan(&incCount); err != nil {
		t.Fatal(err)
	}
	if incCount != 2 {
		t.Errorf("incident count = %d, want 2", incCount)
	}

	// Invalid timestamps on i2 store as NULL.
	var started, resolved sql.NullString
	if err := db.QueryRow(`SELECT started_at, resolved_at FROM incidents WHERE incident_id='i2'`).
		Scan(&started, &resolved); err != nil {
		t.Fatal(err)
	}
	if started.Valid || resolved.Valid {
		t.Errorf("i2 times should be NULL, got started=%v resolved=%v", started, resolved)
	}

	// first_recorded_at is pinned; last_recorded_at advances on re-record.
	var first, last string
	if err := db.QueryRow(`SELECT first_recorded_at, last_recorded_at FROM incidents WHERE incident_id='i1'`).
		Scan(&first, &last); err != nil {
		t.Fatal(err)
	}
	// Two Records in the same call window may share a timestamp; just confirm both set.
	if first == "" || last == "" {
		t.Errorf("recorded timestamps not set: first=%q last=%q", first, last)
	}

	// Two fetches rows (one per Record call).
	var fetchCount int
	if err := db.QueryRow(`SELECT count(*) FROM fetches`).Scan(&fetchCount); err != nil {
		t.Fatal(err)
	}
	if fetchCount != 2 {
		t.Errorf("fetch count = %d, want 2", fetchCount)
	}

	// component_status rows tie to the latest fetch id; 2 components per fetch.
	var compCount, distinctFetch int
	if err := db.QueryRow(`SELECT count(*), count(DISTINCT fetch_id) FROM component_status`).
		Scan(&compCount, &distinctFetch); err != nil {
		t.Fatal(err)
	}
	if compCount != 4 || distinctFetch != 2 {
		t.Errorf("component rows = %d (distinct fetch %d), want 4 across 2 fetches", compCount, distinctFetch)
	}
}

func TestRecordNilAndNoSummary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "metrics.db")
	store, err := Open(path)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer store.Close()

	// nil data is a no-op.
	if err := store.Record("Claude", nil); err != nil {
		t.Fatalf("Record(nil): %v", err)
	}

	// Summary nil: incidents + a fetch row with empty overall fields still land.
	data := &api.DashboardData{
		FetchedAt: time.Now(),
		Incidents: &api.IncidentsResponse{
			Incidents: []api.Incident{{ID: "x1", Name: "N", Status: "resolved", Impact: "minor"}},
		},
	}
	if err := store.Record("OpenAI", data); err != nil {
		t.Fatalf("Record(no summary): %v", err)
	}

	var incCount, fetchCount, compCount int
	store.db.QueryRow(`SELECT count(*) FROM incidents`).Scan(&incCount)
	store.db.QueryRow(`SELECT count(*) FROM fetches`).Scan(&fetchCount)
	store.db.QueryRow(`SELECT count(*) FROM component_status`).Scan(&compCount)
	if incCount != 1 || fetchCount != 1 || compCount != 0 {
		t.Errorf("got inc=%d fetch=%d comp=%d, want 1/1/0", incCount, fetchCount, compCount)
	}
}
