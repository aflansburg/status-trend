package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"status-trend/internal/report"
	"status-trend/internal/storage"
	"status-trend/internal/ui"
)

var (
	stdoutMode bool
	recordMode bool
	onceMode   bool
	dbPath     string
)

var rootCmd = &cobra.Command{
	Use:   "status-trend",
	Short: "Multi-vendor status page dashboard",
	Long:  "A terminal dashboard that visualizes outage and incident data from multiple vendor status pages",
	RunE: func(cmd *cobra.Command, args []string) error {
		recording := recordMode || onceMode

		var store *storage.Store
		var path string
		if recording {
			var err error
			path, err = resolveDBPath()
			if err != nil {
				return err
			}
			store, err = storage.Open(path)
			if err != nil {
				return fmt.Errorf("opening metrics db: %w", err)
			}
			defer store.Close()
		}

		switch {
		case onceMode:
			return report.RecordAll(store, path, os.Stdout)
		case stdoutMode:
			return report.WriteAll(os.Stdout, store)
		default:
			p := tea.NewProgram(ui.NewModel(store))
			if _, err := p.Run(); err != nil {
				return fmt.Errorf("running dashboard: %w", err)
			}
			return nil
		}
	},
}

// resolveDBPath picks the metrics db path: --db flag, then STATUS_TREND_DB env,
// then a default under the OS user config dir.
func resolveDBPath() (string, error) {
	if dbPath != "" {
		return dbPath, nil
	}
	if env := os.Getenv("STATUS_TREND_DB"); env != "" {
		return env, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolving config dir: %w", err)
	}
	return filepath.Join(dir, "status-trend", "metrics.db"), nil
}

func init() {
	rootCmd.Flags().BoolVar(&stdoutMode, "stdout", false, "output all vendor status data to stdout in an LLM-friendly format and exit")
	rootCmd.Flags().BoolVar(&recordMode, "record", false, "record fetched metrics to a local SQLite database")
	rootCmd.Flags().BoolVar(&onceMode, "once", false, "fetch all vendors once, record, print a summary, and exit (implies --record; pairs with cron/launchd)")
	rootCmd.Flags().StringVar(&dbPath, "db", "", "path to the metrics SQLite file (overrides STATUS_TREND_DB and the default)")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
