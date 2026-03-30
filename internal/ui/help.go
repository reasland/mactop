package ui

import "github.com/charmbracelet/lipgloss"

var helpStyle = lipgloss.NewStyle().
	Border(lipgloss.DoubleBorder()).
	BorderForeground(lipgloss.AdaptiveColor{Light: "25", Dark: "39"}).
	Padding(1, 2).
	Align(lipgloss.Left)

func renderHelp(width, height int) string {
	help := headingStyle.Render("mactop Help") + "\n\n" +
		labelStyle.Render("  q, Ctrl+C") + "  Quit\n" +
		labelStyle.Render("  ?        ") + "  Toggle this help\n" +
		labelStyle.Render("  r        ") + "  Reset peak values\n" +
		labelStyle.Render("  g        ") + "  Toggle graphs\n\n" +
		dimStyle.Render("  Data sources: Mach kernel, IOKit, sysctl, SMC\n") +
		dimStyle.Render("  Temperature sensors require access to AppleSMC.\n") +
		dimStyle.Render("  GPU stats read from IOAccelerator service.\n\n") +
		dimStyle.Render("  Press ? to close this help.")

	box := helpStyle.Width(width/2).Render(help)

	// Center the help box.
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, box)
}
