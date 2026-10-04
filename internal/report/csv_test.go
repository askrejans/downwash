package report

import (
	"encoding/csv"
	"github.com/askrejans/downwash/internal/telemetry"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCSVAllFramesEmptyFieldsAndFormulaSafety(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data.csv")
	frames := []telemetry.Frame{{Lat: 57, Lon: 24, AltRelative: 0, Available: &telemetry.Availability{GPS: true, AltRelative: true}, Additional: map[string]any{"model": "=unsafe()", "frame_width_px": 3840}}, {Available: &telemetry.Availability{Camera: true}, ISO: 100, ShutterSpeed: "1/500"}}
	if err := CSV(frames, path); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	rows, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 3 {
		t.Fatal(rows)
	}
	if rows[1][4] != "" || rows[1][5] != "0" || rows[1][6] != "" || rows[2][2] != "" {
		t.Fatal(rows)
	}
	if rows[1][15] != "3840" || rows[1][16] != "'=unsafe()" {
		t.Fatal(rows)
	}
}
