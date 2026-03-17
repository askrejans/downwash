package pipeline

import (
	"os"
	"path/filepath"
)

// FindVideos returns all .MP4 / .mp4 files in dir. If recursive is true, it
// walks the entire directory tree; otherwise only the top level is scanned.
// Transcoded output files (ending in _h264.mp4 or _h265.mp4) are excluded.
func FindVideos(dir string, recursive bool) ([]string, error) {
	var videos []string

	if recursive {
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && IsVideoFile(path) {
				videos = append(videos, path)
			}
			return nil
		})
		return videos, err
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		if !e.IsDir() && IsVideoFile(e.Name()) {
			videos = append(videos, filepath.Join(dir, e.Name()))
		}
	}
	return videos, nil
}

// IsVideoFile returns true for files with .MP4 or .mp4 extensions that are
// not already downwash output files (i.e. not ending in _h264.mp4 etc.).
func IsVideoFile(name string) bool {
	base := filepath.Base(name)
	ext := filepath.Ext(base)
	if ext != ".MP4" && ext != ".mp4" {
		return false
	}
	// Skip already-transcoded downwash outputs.
	for _, suffix := range []string{"_h264.mp4", "_h265.mp4"} {
		if len(base) > len(suffix) &&
			base[len(base)-len(suffix):] == suffix {
			return false
		}
	}
	return true
}
