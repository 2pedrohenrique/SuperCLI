package ui

import "strings"

// Fullscreen shares the normal output renderer and key handlers. Only the
// viewport changes; running processes, log history and selection stay intact.
func (m Model) renderFullscreenOutput(width, height int) string {
	if height < 6 {
		return fillLines([]string{"OUTPUT • F7/Esc tiles", "Resize to see logs"}, width, height)
	}
	w, p, _ := m.selection()
	header := " " + logoStyle.Render("◆ OUTPUT") + mutedStyle.Render(" • FULLSCREEN  │  ") + w.Name + " › " + p.Name
	if branch := m.currentBranch(); branch != "" {
		header += "  " + infoStyle.Render("git:"+branch)
	}
	status := " ● " + m.status
	if m.statusError {
		status = failureStyle.Render(status)
	}
	footer := " F7/Esc tiles │ Tab process │ c capture │ l clear │ End follow │ ? help"
	return strings.Join([]string{
		fit(header, width),
		m.renderMain(width, height-3),
		fit(status, width),
		mutedStyle.Render(fit(footer, width)),
	}, "\n")
}
