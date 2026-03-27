package cmd

import (
	"fmt"
	"os"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"
	"status-trend/internal/ui"
)

var rootCmd = &cobra.Command{
	Use:   "status-trend",
	Short: "Multi-vendor status page dashboard",
	Long:  "A terminal dashboard that visualizes outage and incident data from multiple vendor status pages",
	RunE: func(cmd *cobra.Command, args []string) error {
		p := tea.NewProgram(ui.NewModel())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("running dashboard: %w", err)
		}
		return nil
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}
