package cmd

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"status-trend/internal/report"
	"status-trend/internal/ui"
)

var stdoutMode bool

var rootCmd = &cobra.Command{
	Use:   "status-trend",
	Short: "Multi-vendor status page dashboard",
	Long:  "A terminal dashboard that visualizes outage and incident data from multiple vendor status pages",
	RunE: func(cmd *cobra.Command, args []string) error {
		if stdoutMode {
			return report.WriteAll(os.Stdout)
		}

		p := tea.NewProgram(ui.NewModel())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running dashboard: %w", err)
		}
		return nil
	},
}

func init() {
	rootCmd.Flags().BoolVar(&stdoutMode, "stdout", false, "output all vendor status data to stdout in an LLM-friendly format and exit")
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
