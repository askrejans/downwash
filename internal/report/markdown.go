// Package report formats flight data into human-readable outputs: Markdown
// briefings and aviation-themed PDF post-flight reports.
package report

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/askrejans/downwash/internal/telemetry"
)

// Markdown writes a post-flight briefing as a Markdown file to outputPath.
// videoName is typically the basename of the source MP4 file. codec is the
// detected or transcoded video codec string (e.g. "hevc", "h264").
func Markdown(stats telemetry.FlightStats, videoName, codec, outputPath string) error {
	sb := new(strings.Builder)

	// Title block.
	sb.WriteString("# Post-Flight Briefing\n\n")
	sb.WriteString(fmt.Sprintf("> **Source:** `%s`", videoName))
	if codec != "" {
		sb.WriteString(fmt.Sprintf("  |  **Codec:** `%s`", strings.ToUpper(codec)))
	}
	sb.WriteString("\n")
	if !stats.StartTime.IsZero() {
		sb.WriteString(fmt.Sprintf("> **Date:** %s UTC\n",
			stats.StartTime.UTC().Format("2006-01-02 15:04:05")))
	}
	sb.WriteString(fmt.Sprintf("> **Generated:** %s by [downwash](https://github.com/askrejans/downwash)\n\n",
		time.Now().UTC().Format("2006-01-02 15:04 UTC")))

	sb.WriteString("---\n\n")

	// Flight overview.
	sb.WriteString("## Flight Overview\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|:---|:---|\n")
	sb.WriteString(fmt.Sprintf("| **Duration** | `%s` |\n", formatDuration(stats.Duration)))
	sb.WriteString(fmt.Sprintf("| **Total Distance** | `%.0f m` / `%.2f km` |\n", stats.DistanceM, stats.DistanceM/1000))
	sb.WriteString(fmt.Sprintf("| **Frames** | `%d` |\n", stats.FrameCount))
	sb.WriteString(fmt.Sprintf("| **GPS Points** | `%d` |\n", stats.GPSPointCount))
	if !stats.StartTime.IsZero() && !stats.EndTime.IsZero() {
		sb.WriteString(fmt.Sprintf("| **Start Time** | `%s` |\n", stats.StartTime.UTC().Format("15:04:05 UTC")))
		sb.WriteString(fmt.Sprintf("| **End Time** | `%s` |\n", stats.EndTime.UTC().Format("15:04:05 UTC")))
	}
	sb.WriteString("\n---\n\n")

	// GPS & Navigation.
	sb.WriteString("## GPS & Navigation\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|:---|:---|\n")
	sb.WriteString(fmt.Sprintf("| **Start Position** | `%.6fN, %.6fE` |\n", stats.StartLat, stats.StartLon))
	sb.WriteString(fmt.Sprintf("| **End Position** | `%.6fN, %.6fE` |\n", stats.EndLat, stats.EndLon))
	sb.WriteString(fmt.Sprintf("| **Total Distance** | `%.0f m` / `%.2f km` |\n", stats.DistanceM, stats.DistanceM/1000))
	sb.WriteString("\n---\n\n")

	// Altitude.
	sb.WriteString("## Altitude\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|:---|:---|\n")
	sb.WriteString(fmt.Sprintf("| **Max Altitude ASL** | `%.1f m` |\n", stats.MaxAltASL))
	sb.WriteString(fmt.Sprintf("| **Min Altitude ASL** | `%.1f m` |\n", stats.MinAltASL))
	sb.WriteString(fmt.Sprintf("| **Max Altitude AGL** | `%.1f m` |\n", stats.MaxAltAGL))
	sb.WriteString(fmt.Sprintf("| **Min Altitude AGL** | `%.1f m` |\n", stats.MinAltAGL))
	sb.WriteString(fmt.Sprintf("| **Altitude Range ASL** | `%.1f m` |\n", stats.MaxAltASL-stats.MinAltASL))
	sb.WriteString(fmt.Sprintf("| **Total Climb** | `%.0f m` |\n", stats.AltGainM))
	sb.WriteString(fmt.Sprintf("| **Total Descent** | `%.0f m` |\n", stats.AltLossM))
	sb.WriteString(fmt.Sprintf("| **Max Climb Rate** | `%.1f m/s` |\n", stats.MaxClimbMS))
	sb.WriteString(fmt.Sprintf("| **Max Descent Rate** | `%.1f m/s` |\n", stats.MaxDescentMS))
	sb.WriteString("\n---\n\n")

	// Speed.
	sb.WriteString("## Speed\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|:---|:---|\n")
	sb.WriteString(fmt.Sprintf("| **Max Speed** | `%.1f m/s` / `%.1f km/h` |\n",
		stats.MaxSpeedMS, stats.MaxSpeedMS*3.6))
	sb.WriteString(fmt.Sprintf("| **Avg Speed** | `%.1f m/s` / `%.1f km/h` |\n",
		stats.AvgSpeedMS, stats.AvgSpeedMS*3.6))
	sb.WriteString("\n---\n\n")

	// Flight dynamics.
	sb.WriteString("## Flight Dynamics\n\n")
	sb.WriteString("| Metric | Value |\n")
	sb.WriteString("|:---|:---|\n")
	sb.WriteString(fmt.Sprintf("| **Max Distance from Home** | `%.0f m` / `%.2f km` |\n",
		stats.MaxHomeDist, stats.MaxHomeDist/1000))
	sb.WriteString(fmt.Sprintf("| **Max Roll** | `%.1f\u00b0` |\n", stats.MaxRoll))
	sb.WriteString(fmt.Sprintf("| **Max Pitch** | `%.1f\u00b0` |\n", stats.MaxPitch))
	sb.WriteString(fmt.Sprintf("| **Max Yaw Rate** | `%.1f\u00b0/s` |\n", stats.MaxYawRate))
	sb.WriteString("\n---\n\n")

	// Camera settings.
	sb.WriteString("## Camera Settings\n\n")
	sb.WriteString("| Setting | Value |\n")
	sb.WriteString("|:---|:---|\n")
	if codec != "" {
		sb.WriteString(fmt.Sprintf("| **Codec** | `%s` |\n", strings.ToUpper(codec)))
	}
	if stats.ISO > 0 {
		sb.WriteString(fmt.Sprintf("| **ISO** | `%d` |\n", stats.ISO))
	}
	if stats.ShutterSpeed != "" {
		sb.WriteString(fmt.Sprintf("| **Shutter Speed** | `%s` |\n", stats.ShutterSpeed))
	}
	if stats.FNumber > 0 {
		sb.WriteString(fmt.Sprintf("| **Aperture** | `f/%.1f` |\n", stats.FNumber))
	}
	if stats.ColorTemp > 0 {
		sb.WriteString(fmt.Sprintf("| **Color Temp** | `%d K` |\n", stats.ColorTemp))
	}
	sb.WriteString("\n---\n\n")

	// Footer.
	sb.WriteString("*Map tiles by [CartoDB](https://carto.com/) under CC BY 3.0. Map data (c) [OpenStreetMap](https://www.openstreetmap.org/copyright) contributors.*\n\n")
	sb.WriteString("*Generated by [downwash](https://github.com/askrejans/downwash) -- DJI post-flight analysis toolkit*\n")

	return os.WriteFile(outputPath, []byte(sb.String()), 0o644)
}

// formatDuration formats a time.Duration as "3m 12s".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second)
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm %ds", h, m, s)
	}
	if m > 0 {
		return fmt.Sprintf("%dm %ds", m, s)
	}
	return fmt.Sprintf("%ds", s)
}
