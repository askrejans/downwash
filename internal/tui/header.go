package tui

import "fmt"

const asciiArt = `
 ____   _____        ___   ___        ___    ____  _   _
|  _ \ / _ \ \      / / \ | \ \      / / \  / ___|| | | |
| | | | | | \ \ /\ / /|  \| |\ \ /\ / / _ \ \___ \| |_| |
| |_| | |_| |\ V  V / | |\  | \ V  V / ___ \ ___) |  _  |
|____/ \___/  \_/\_/  |_| \_|  \_/\_/_/   \_\____/|_| |_|`

// renderHeader returns the styled ASCII header with version.
func renderHeader(version string) string {
	art := headerStyle.Render(asciiArt)
	ver := versionStyle.Render(fmt.Sprintf("  v%s", version))
	sub := subtitleStyle.Render("  DJI Post-Flight Analysis Toolkit")
	return art + ver + "\n" + sub + "\n"
}
