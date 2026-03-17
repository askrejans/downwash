package report

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jung-kurt/gofpdf"

	"github.com/askrejans/downwash/internal/telemetry"
)

// ---------- constants -------------------------------------------------------

const (
	pdfPageW  = 297.0 // A4 landscape width  (mm)
	pdfPageH  = 210.0 // A4 landscape height (mm)
	pdfMargin = 14.0
)

// tableRow is a key/value pair used in detail tables.
type tableRow struct{ k, v string }

// colour palette (RGB 0–255)
var (
	colHeaderBg = [3]int{18, 18, 28}   // near-black
	colAccent   = [3]int{77, 166, 255} // sky blue
	colRowLight = [3]int{28, 28, 42}   // dark row
	colRowDark  = [3]int{22, 22, 35}   // slightly darker row
	colText     = [3]int{200, 200, 220} // body text
	colSubhead  = [3]int{150, 180, 255} // section subheading
)

// ---------- PDF ─────────────────────────────────────────────────────────────

// PDF generates a three-page aviation-themed post-flight briefing PDF.
//
//   - Page 1: Cover — call-sign banner, key stats grid, GPS track chart.
//   - Page 2: Altitude profiles (ASL + AGL charts stacked).
//   - Page 3: Tabular telemetry — camera settings, attitude extremes, footer.
//
// altitudePNG and trackPNG are paths to pre-rendered chart images; pass ""
// to skip the corresponding image.
func PDF(
	stats telemetry.FlightStats,
	videoName, codec string,
	altitudePNG, trackPNG string,
	outputPath string,
) error {
	pdf := gofpdf.New("L", "mm", "A4", "")
	pdf.SetAutoPageBreak(false, 0)

	// ── Page 1: Cover + GPS track ────────────────────────────────────────────
	addPage(pdf)
	drawCoverPage(pdf, stats, videoName, codec, trackPNG)

	// ── Page 2: Altitude profiles ────────────────────────────────────────────
	addPage(pdf)
	drawAltitudePage(pdf, stats, altitudePNG)

	// ── Page 3: Detailed telemetry table ────────────────────────────────────
	addPage(pdf)
	drawDetailPage(pdf, stats, codec)

	return pdf.OutputFileAndClose(outputPath)
}

// ---------- page builders ---------------------------------------------------

func drawCoverPage(pdf *gofpdf.Fpdf, stats telemetry.FlightStats, videoName, codec, trackPNG string) {
	w, h := pdfPageW, pdfPageH

	// Dark background.
	setFill(pdf, colHeaderBg)
	pdf.Rect(0, 0, w, h, "F")

	// Top banner.
	setFill(pdf, colAccent)
	pdf.Rect(0, 0, w, 22, "F")

	// Banner text.
	pdf.SetFont("Helvetica", "B", 16)
	setTextColor(pdf, colHeaderBg)
	pdf.SetXY(pdfMargin, 5)
	pdf.CellFormat(w-pdfMargin*2, 12, "POST-FLIGHT BRIEFING  |  DOWNWASH", "", 0, "L", false, 0, "")

	// Sub-banner: date/time.
	var dateStr string
	if !stats.StartTime.IsZero() {
		dateStr = stats.StartTime.UTC().Format("2006-01-02  15:04 UTC")
	} else {
		dateStr = time.Now().UTC().Format("2006-01-02  15:04 UTC")
	}
	pdf.SetFont("Helvetica", "", 10)
	pdf.SetXY(pdfMargin, 14)
	pdf.CellFormat(w-pdfMargin*2, 6, dateStr, "", 0, "L", false, 0, "")

	// Video filename strip.
	setFill(pdf, colRowDark)
	pdf.Rect(0, 22, w, 9, "F")
	pdf.SetFont("Courier", "", 8)
	setTextColor(pdf, colSubhead)
	pdf.SetXY(pdfMargin, 23.5)
	pdf.CellFormat(w-pdfMargin*2, 6, "SOURCE: "+truncate(videoName, 80), "", 0, "L", false, 0, "")

	// ── Stats grid (left column) ─────────────────────────────────────────────
	const colX = pdfMargin
	const colY = 35.0
	const rowH = 12.0

	rows := []tableRow{
		{"DURATION", formatDuration(stats.Duration)},
		{"DISTANCE", fmt.Sprintf("%.0f m  /  %.2f km", stats.DistanceM, stats.DistanceM/1000)},
		{"MAX ALT (ASL)", fmt.Sprintf("%.1f m", stats.MaxAltASL)},
		{"MAX ALT (AGL)", fmt.Sprintf("%.1f m", stats.MaxAltAGL)},
		{"MAX SPEED", fmt.Sprintf("%.1f m/s  (%.1f km/h)", stats.MaxSpeedMS, stats.MaxSpeedMS*3.6)},
		{"AVG SPEED", fmt.Sprintf("%.1f m/s  (%.1f km/h)", stats.AvgSpeedMS, stats.AvgSpeedMS*3.6)},
		{"GPS POINTS", fmt.Sprintf("%d", stats.GPSPointCount)},
		{"FRAMES", fmt.Sprintf("%d", stats.FrameCount)},
	}

	labelW := 48.0
	valW := 60.0
	for i, r := range rows {
		y := colY + float64(i)*rowH
		if i%2 == 0 {
			setFill(pdf, colRowLight)
		} else {
			setFill(pdf, colRowDark)
		}
		pdf.Rect(colX, y, labelW+valW, rowH, "F")

		// Label.
		pdf.SetFont("Helvetica", "B", 8)
		setTextColor(pdf, colSubhead)
		pdf.SetXY(colX+2, y+2)
		pdf.CellFormat(labelW-2, rowH-4, r.k, "", 0, "L", false, 0, "")

		// Value.
		pdf.SetFont("Helvetica", "", 9)
		setTextColor(pdf, colText)
		pdf.SetXY(colX+labelW+2, y+2)
		pdf.CellFormat(valW-4, rowH-4, r.v, "", 0, "L", false, 0, "")
	}

	// Start/End position.
	posY := colY + float64(len(rows))*rowH + 4
	pdf.SetFont("Helvetica", "B", 8)
	setTextColor(pdf, colSubhead)
	pdf.SetXY(colX, posY)
	pdf.CellFormat(labelW+valW, 6, "START  "+coordStr(stats.StartLat, stats.StartLon), "", 1, "L", false, 0, "")
	pdf.SetXY(colX, posY+7)
	pdf.CellFormat(labelW+valW, 6, "END    "+coordStr(stats.EndLat, stats.EndLon), "", 1, "L", false, 0, "")

	// ── Track image (right side) ─────────────────────────────────────────────
	if trackPNG != "" {
		const imgX = 145.0
		const imgY = 31.0
		const imgW = 140.0
		const imgH = 168.0
		embedImage(pdf, trackPNG, imgX, imgY, imgW, imgH)
	}

	// Footer.
	drawFooter(pdf, 1, 3)
}

func drawAltitudePage(pdf *gofpdf.Fpdf, stats telemetry.FlightStats, altitudePNG string) {
	w, h := pdfPageW, pdfPageH

	setFill(pdf, colHeaderBg)
	pdf.Rect(0, 0, w, h, "F")

	// Mini header bar.
	setFill(pdf, colRowDark)
	pdf.Rect(0, 0, w, 14, "F")
	pdf.SetFont("Helvetica", "B", 11)
	setTextColor(pdf, colAccent)
	pdf.SetXY(pdfMargin, 3)
	pdf.CellFormat(w-pdfMargin*2, 9, "ALTITUDE PROFILE", "", 0, "L", false, 0, "")

	// Stats strip.
	strip := []tableRow{
		{"ASL MAX", fmt.Sprintf("%.1f m", stats.MaxAltASL)},
		{"ASL MIN", fmt.Sprintf("%.1f m", stats.MinAltASL)},
		{"AGL MAX", fmt.Sprintf("%.1f m", stats.MaxAltAGL)},
		{"AGL MIN", fmt.Sprintf("%.1f m", stats.MinAltAGL)},
		{"DURATION", formatDuration(stats.Duration)},
	}
	stripX := pdfMargin
	stripY := 15.0
	stripW := (w - pdfMargin*2) / float64(len(strip))
	for i, item := range strip {
		if i%2 == 0 {
			setFill(pdf, colRowLight)
		} else {
			setFill(pdf, colRowDark)
		}
		pdf.Rect(stripX+float64(i)*stripW, stripY, stripW, 14, "F")
		pdf.SetFont("Helvetica", "B", 7)
		setTextColor(pdf, colSubhead)
		pdf.SetXY(stripX+float64(i)*stripW+2, stripY+1)
		pdf.CellFormat(stripW-4, 5, item.k, "", 0, "L", false, 0, "")
		pdf.SetFont("Helvetica", "", 10)
		setTextColor(pdf, colText)
		pdf.SetXY(stripX+float64(i)*stripW+2, stripY+6)
		pdf.CellFormat(stripW-4, 7, item.v, "", 0, "L", false, 0, "")
	}

	// Altitude chart image.
	if altitudePNG != "" {
		embedImage(pdf, altitudePNG, pdfMargin, 31, w-pdfMargin*2, h-31-18)
	}

	drawFooter(pdf, 2, 3)
}

func drawDetailPage(pdf *gofpdf.Fpdf, stats telemetry.FlightStats, codec string) {
	w, h := pdfPageW, pdfPageH

	setFill(pdf, colHeaderBg)
	pdf.Rect(0, 0, w, h, "F")

	// Header.
	setFill(pdf, colRowDark)
	pdf.Rect(0, 0, w, 14, "F")
	pdf.SetFont("Helvetica", "B", 11)
	setTextColor(pdf, colAccent)
	pdf.SetXY(pdfMargin, 3)
	pdf.CellFormat(w-pdfMargin*2, 9, "DETAILED TELEMETRY", "", 0, "L", false, 0, "")

	y := 18.0

	// Camera settings section.
	y = drawSectionHeader(pdf, "CAMERA SETTINGS", y)
	camRows := []tableRow{
		{"Codec", strings.ToUpper(codec)},
		{"ISO", fmt.Sprintf("%d", stats.ISO)},
		{"Shutter Speed", stats.ShutterSpeed},
		{"f-number", fmt.Sprintf("f/%.1f", stats.FNumber)},
		{"Color Temperature", fmt.Sprintf("%d K", stats.ColorTemp)},
	}
	y = drawKVTable(pdf, camRows, y+1)

	y += 6
	y = drawSectionHeader(pdf, "GPS & NAVIGATION", y)
	gpsRows := []tableRow{
		{"Start Lat/Lon", coordStr(stats.StartLat, stats.StartLon)},
		{"End Lat/Lon", coordStr(stats.EndLat, stats.EndLon)},
		{"Total Distance", fmt.Sprintf("%.2f km  (%.0f m)", stats.DistanceM/1000, stats.DistanceM)},
		{"GPS Points", fmt.Sprintf("%d", stats.GPSPointCount)},
		{"Frame Count", fmt.Sprintf("%d", stats.FrameCount)},
	}
	y = drawKVTable(pdf, gpsRows, y+1)

	y += 6
	y = drawSectionHeader(pdf, "PERFORMANCE", y)
	perfRows := []tableRow{
		{"Max Speed", fmt.Sprintf("%.1f m/s  (%.1f km/h)", stats.MaxSpeedMS, stats.MaxSpeedMS*3.6)},
		{"Avg Speed", fmt.Sprintf("%.1f m/s  (%.1f km/h)", stats.AvgSpeedMS, stats.AvgSpeedMS*3.6)},
		{"Max Alt ASL", fmt.Sprintf("%.1f m", stats.MaxAltASL)},
		{"Min Alt ASL", fmt.Sprintf("%.1f m", stats.MinAltASL)},
		{"Max Alt AGL", fmt.Sprintf("%.1f m", stats.MaxAltAGL)},
		{"Min Alt AGL", fmt.Sprintf("%.1f m", stats.MinAltAGL)},
	}
	y = drawKVTable(pdf, perfRows, y+1)

	// Disclaimer.
	pdf.SetFont("Helvetica", "I", 7)
	setTextColor(pdf, colRowLight)
	pdf.SetXY(pdfMargin, h-24)
	pdf.MultiCell(w-pdfMargin*2, 4,
		"NOTICE: This briefing is generated from embedded drone telemetry for informational purposes only. "+
			"It is not a certified aviation document. Always comply with local airspace regulations and "+
			"the drone manufacturer's safety guidelines.",
		"", "L", false)

	drawFooter(pdf, 3, 3)
}

// ---------- layout helpers --------------------------------------------------

func addPage(pdf *gofpdf.Fpdf) {
	pdf.AddPage()
}

func drawFooter(pdf *gofpdf.Fpdf, page, total int) {
	w := pdfPageW
	y := pdfPageH - 10.0

	setFill(pdf, colRowDark)
	pdf.Rect(0, y, w, 10, "F")

	pdf.SetFont("Helvetica", "", 7)
	setTextColor(pdf, colSubhead)
	pdf.SetXY(pdfMargin, y+2)
	pdf.CellFormat(w/2-pdfMargin, 6,
		fmt.Sprintf("Generated by downwash  -  %s", time.Now().UTC().Format("2006-01-02 15:04 UTC")),
		"", 0, "L", false, 0, "")
	pdf.SetXY(w/2, y+2)
	pdf.CellFormat(w/2-pdfMargin, 6, fmt.Sprintf("Page %d / %d", page, total), "", 0, "R", false, 0, "")
}

func drawSectionHeader(pdf *gofpdf.Fpdf, text string, y float64) float64 {
	w := pdfPageW
	setFill(pdf, colAccent)
	pdf.Rect(pdfMargin, y, w-pdfMargin*2, 8, "F")
	pdf.SetFont("Helvetica", "B", 9)
	setTextColor(pdf, colHeaderBg)
	pdf.SetXY(pdfMargin+3, y+1)
	pdf.CellFormat(w-pdfMargin*2-6, 6, text, "", 0, "L", false, 0, "")
	return y + 8
}

func drawKVTable(pdf *gofpdf.Fpdf, rows []tableRow, y float64) float64 {
	labelW := 55.0
	valW := 100.0
	rowH := 8.0

	for i, r := range rows {
		if i%2 == 0 {
			setFill(pdf, colRowLight)
		} else {
			setFill(pdf, colRowDark)
		}
		pdf.Rect(pdfMargin, y, labelW+valW, rowH, "F")

		pdf.SetFont("Helvetica", "B", 8)
		setTextColor(pdf, colSubhead)
		pdf.SetXY(pdfMargin+2, y+1.5)
		pdf.CellFormat(labelW-4, rowH-3, r.k, "", 0, "L", false, 0, "")

		pdf.SetFont("Helvetica", "", 8)
		setTextColor(pdf, colText)
		pdf.SetXY(pdfMargin+labelW+2, y+1.5)
		pdf.CellFormat(valW-4, rowH-3, r.v, "", 0, "L", false, 0, "")

		y += rowH
	}
	return y
}

// embedImage registers and draws a PNG image into the PDF at the given
// coordinates. Errors (missing file, unsupported format) are non-fatal so a
// missing chart doesn't abort the whole PDF. The image is registered directly
// via gofpdf's ImageOptions which reads the file by path.
func embedImage(pdf *gofpdf.Fpdf, path string, x, y, w, h float64) {
	if _, err := os.Stat(path); err != nil {
		return // file does not exist — skip silently
	}
	imgOpts := gofpdf.ImageOptions{ImageType: "PNG", ReadDpi: true}
	pdf.ImageOptions(path, x, y, w, h, false, imgOpts, 0, "")
}

// ---------- colour helpers --------------------------------------------------

func setFill(pdf *gofpdf.Fpdf, c [3]int) {
	pdf.SetFillColor(c[0], c[1], c[2])
}

func setTextColor(pdf *gofpdf.Fpdf, c [3]int) {
	pdf.SetTextColor(c[0], c[1], c[2])
}

// ---------- string helpers --------------------------------------------------

func coordStr(lat, lon float64) string {
	latDir := "N"
	if lat < 0 {
		latDir = "S"
		lat = -lat
	}
	lonDir := "E"
	if lon < 0 {
		lonDir = "W"
		lon = -lon
	}
	deg := "\xb0" // degree symbol in latin-1 (gofpdf uses CP1252)
	return fmt.Sprintf("%.6f%s%s  %.6f%s%s", lat, deg, latDir, lon, deg, lonDir)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "…" + s[len(s)-n+1:]
}
