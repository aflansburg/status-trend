package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

var (
	// Colors
	colorGreen    = lipgloss.Color("#00ff87")
	colorYellow   = lipgloss.Color("#ffff00")
	colorOrange   = lipgloss.Color("#ff8700")
	colorRed      = lipgloss.Color("#ff005f")
	colorCyan     = lipgloss.Color("#00d7ff")
	colorDim      = lipgloss.Color("#626262")
	colorWhite    = lipgloss.Color("#ffffff")
	colorDarkGray = lipgloss.Color("#3a3a3a")
	colorBorder   = lipgloss.Color("#0f3460")

	// Text styles
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorCyan).
			PaddingLeft(1)

	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(1, 2)

	valueStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(colorWhite)

	activeIndicator = lipgloss.NewStyle().
			Foreground(colorRed).
			Bold(true)

	resolvedIndicator = lipgloss.NewStyle().
				Foreground(colorGreen)

	helpStyle = lipgloss.NewStyle().
			Foreground(colorDim).
			PaddingLeft(1)
)

func statusColor(status string) color.Color {
	switch status {
	case "operational":
		return colorGreen
	case "degraded_performance":
		return colorYellow
	case "partial_outage":
		return colorOrange
	case "major_outage":
		return colorRed
	default:
		return colorDim
	}
}

func impactColor(impact string) color.Color {
	switch impact {
	case "none":
		return colorDim
	case "minor":
		return colorYellow
	case "major":
		return colorOrange
	case "critical":
		return colorRed
	default:
		return colorDim
	}
}

func statusDot(status string) string {
	return lipgloss.NewStyle().Foreground(statusColor(status)).Render("●")
}

func impactDot(impact string) string {
	return lipgloss.NewStyle().Foreground(impactColor(impact)).Render("■")
}
