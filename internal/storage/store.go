// Package storage records fetched vendor metrics to a local SQLite file.
//
// The store uses the pure-Go modernc.org/sqlite driver, so the app builds with
// CGO_ENABLED=0. Query the file from DuckDB with:
//
//	INSTALL sqlite; LOAD sqlite;
//	ATTACH 'metrics.db' AS m (TYPE sqlite);
package storage

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"status-trend/internal/api"
)

// Store writes vendor metrics to a SQLite database.
type Store struct {
	db *sql.DB
	mu sync.Mutex // serializes Record transactions across goroutines
}

// Open creates or opens the SQLite file at path, creating the parent directory,
// applying pragmas, and running the table migrations.
func Open(path string) (*Store, error) {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("creating db directory: %w", err)
		}
	}

	// Build the DSN safely — the default macOS path contains a space.
	q := url.Values{}
	q.Add("_pragma", "busy_timeout(5000)")
	q.Add("_pragma", "journal_mode(WAL)")
	q.Add("_pragma", "foreign_keys(ON)")
	dsn := "file:" + path + "?" + q.Encode()

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening db: %w", err)
	}
	db.SetMaxOpenConns(1) // single writer; modernc.org/sqlite prefers this

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("pinging db: %w", err)
	}

	if _, err := db.ExecContext(ctx, schemaDDL); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating schema: %w", err)
	}

	return &Store{db: db}, nil
}

// Close closes the underlying database.
func (s *Store) Close() error {
	return s.db.Close()
}

// Record upserts the vendor's incidents and writes one fetch snapshot plus its
// component-status rows, all in one transaction.
func (s *Store) Record(vendorName string, data *api.DashboardData) error {
	if data == nil {
		return nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("beginning tx: %w", err)
	}
	defer tx.Rollback() // no-op after Commit

	now := time.Now().UTC().Format(time.RFC3339)

	if err := recordIncidents(ctx, tx, vendorName, data, now); err != nil {
		return err
	}

	fetchID, err := recordFetch(ctx, tx, vendorName, data)
	if err != nil {
		return err
	}

	if err := recordComponents(ctx, tx, vendorName, data, fetchID); err != nil {
		return err
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("committing tx: %w", err)
	}
	return nil
}

const upsertIncident = `
INSERT INTO incidents
  (vendor, incident_id, name, status, impact, region_code, shortlink,
   created_at, started_at, resolved_at, monitoring_at, updated_at,
   first_recorded_at, last_recorded_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(vendor, incident_id) DO UPDATE SET
  name             = excluded.name,
  status           = excluded.status,
  impact           = excluded.impact,
  region_code      = excluded.region_code,
  shortlink        = excluded.shortlink,
  created_at       = excluded.created_at,
  started_at       = excluded.started_at,
  resolved_at      = excluded.resolved_at,
  monitoring_at    = excluded.monitoring_at,
  updated_at       = excluded.updated_at,
  last_recorded_at = excluded.last_recorded_at
`

func recordIncidents(ctx context.Context, tx *sql.Tx, vendor string, data *api.DashboardData, now string) error {
	stmt, err := tx.PrepareContext(ctx, upsertIncident)
	if err != nil {
		return fmt.Errorf("preparing incident upsert: %w", err)
	}
	defer stmt.Close()

	seen := make(map[string]bool)
	record := func(inc api.Incident) error {
		if inc.ID == "" || seen[inc.ID] {
			return nil
		}
		seen[inc.ID] = true
		_, err := stmt.ExecContext(ctx,
			vendor, inc.ID, inc.Name, inc.Status, inc.Impact, inc.RegionCode, inc.Shortlink,
			nullableTime(inc.CreatedAt), nullableTime(inc.StartedAt),
			nullableTimePtr(inc.ResolvedAt), nullableTimePtr(inc.MonitoringAt),
			nullableTime(inc.UpdatedAt),
			now, now,
		)
		if err != nil {
			return fmt.Errorf("upserting incident %s: %w", inc.ID, err)
		}
		return nil
	}

	if data.Incidents != nil {
		for _, inc := range data.Incidents.Incidents {
			if err := record(inc); err != nil {
				return err
			}
		}
	}
	if data.Summary != nil {
		for _, inc := range data.Summary.Incidents {
			if err := record(inc); err != nil {
				return err
			}
		}
		for _, inc := range data.Summary.ScheduledMaintenances {
			if err := record(inc); err != nil {
				return err
			}
		}
	}
	return nil
}

func recordFetch(ctx context.Context, tx *sql.Tx, vendor string, data *api.DashboardData) (int64, error) {
	var indicator, description string
	if data.Summary != nil {
		indicator = data.Summary.Status.Indicator
		description = data.Summary.Status.Description
	}
	fetchedAt := data.FetchedAt
	if fetchedAt.IsZero() {
		fetchedAt = time.Now()
	}

	res, err := tx.ExecContext(ctx,
		`INSERT INTO fetches (vendor, fetched_at, overall_indicator, overall_description)
		 VALUES (?,?,?,?)`,
		vendor, fetchedAt.UTC().Format(time.RFC3339), indicator, description,
	)
	if err != nil {
		return 0, fmt.Errorf("inserting fetch: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("reading fetch id: %w", err)
	}
	return id, nil
}

func recordComponents(ctx context.Context, tx *sql.Tx, vendor string, data *api.DashboardData, fetchID int64) error {
	if data.Summary == nil || len(data.Summary.Components) == 0 {
		return nil
	}

	stmt, err := tx.PrepareContext(ctx,
		`INSERT INTO component_status (fetch_id, vendor, component_id, name, status, is_group)
		 VALUES (?,?,?,?,?,?)
		 ON CONFLICT(fetch_id, component_id) DO NOTHING`)
	if err != nil {
		return fmt.Errorf("preparing component insert: %w", err)
	}
	defer stmt.Close()

	for _, c := range data.Summary.Components {
		if c.ID == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, fetchID, vendor, c.ID, c.Name, c.Status, boolToInt(c.Group)); err != nil {
			return fmt.Errorf("inserting component %s: %w", c.ID, err)
		}
	}
	return nil
}

// nullableTime returns an RFC3339 UTC string, or nil for a zero/invalid time.
// The year<2000 guard mirrors report.validStart.
func nullableTime(t time.Time) any {
	if t.IsZero() || t.Year() < 2000 {
		return nil
	}
	return t.UTC().Format(time.RFC3339)
}

func nullableTimePtr(t *time.Time) any {
	if t == nil {
		return nil
	}
	return nullableTime(*t)
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
