package chart

import (
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"net/http"
	"time"
)

// tileProvider fetches dark-themed OSM map tiles.
// Uses CartoDB dark_matter (no API key required, free for open-source).
const tileURL = "https://basemaps.cartocdn.com/dark_all/%d/%d/%d.png"

// httpClient with a short timeout so tile fetch failures don't stall the pipeline.
var httpClient = &http.Client{Timeout: 10 * time.Second}

// fetchMapBackground downloads dark OSM tiles covering the given lat/lon
// bounding box and composites them into a single image of the given pixel size.
// Returns nil if any tile fails to download (caller should fall back).
func fetchMapBackground(minLat, maxLat, minLon, maxLon float64, widthPx, heightPx int) image.Image {
	// Pick a zoom level that gives reasonable detail.
	zoom := pickZoom(minLat, maxLat, minLon, maxLon, widthPx)

	// Convert lat/lon bounds to tile coordinates.
	minTX, minTY := latLonToTile(maxLat, minLon, zoom) // NW corner
	maxTX, maxTY := latLonToTile(minLat, maxLon, zoom) // SE corner

	// Clamp to reasonable tile count (max 6x6 = 36 tiles).
	if maxTX-minTX > 5 || maxTY-minTY > 5 {
		return nil
	}

	tilesX := maxTX - minTX + 1
	tilesY := maxTY - minTY + 1

	// Download and compose tiles.
	composite := image.NewRGBA(image.Rect(0, 0, tilesX*256, tilesY*256))
	// Fill with dark background in case of partial failures.
	draw.Draw(composite, composite.Bounds(),
		&image.Uniform{color.RGBA{R: 18, G: 18, B: 28, A: 255}},
		image.Point{}, draw.Src)

	for ty := minTY; ty <= maxTY; ty++ {
		for tx := minTX; tx <= maxTX; tx++ {
			tile := fetchTile(zoom, tx, ty)
			if tile == nil {
				return nil // any failure → fallback
			}
			offsetX := (tx - minTX) * 256
			offsetY := (ty - minTY) * 256
			draw.Draw(composite,
				image.Rect(offsetX, offsetY, offsetX+256, offsetY+256),
				tile, image.Point{}, draw.Over)
		}
	}

	// Now crop/scale the composite to match the exact lat/lon bounds.
	// Calculate pixel positions of our exact bounds within the tile grid.
	nwPixX, nwPixY := latLonToPixel(maxLat, minLon, zoom)
	sePixX, sePixY := latLonToPixel(minLat, maxLon, zoom)

	// Offset relative to the tile grid origin.
	originPixX := float64(minTX * 256)
	originPixY := float64(minTY * 256)

	cropX0 := int(nwPixX - originPixX)
	cropY0 := int(nwPixY - originPixY)
	cropX1 := int(sePixX - originPixX)
	cropY1 := int(sePixY - originPixY)

	if cropX1 <= cropX0 || cropY1 <= cropY0 {
		return nil
	}

	// Crop and scale to target size.
	cropped := composite.SubImage(image.Rect(cropX0, cropY0, cropX1, cropY1))
	return scaleImage(cropped, widthPx, heightPx)
}

// fetchTile downloads a single 256x256 map tile. Returns nil on any error.
func fetchTile(zoom, x, y int) image.Image {
	url := fmt.Sprintf(tileURL, zoom, x, y)
	resp, err := httpClient.Get(url)
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	img, err := png.Decode(resp.Body)
	if err != nil {
		return nil
	}
	return img
}

// latLonToTile converts lat/lon to OSM tile coordinates at the given zoom.
func latLonToTile(lat, lon float64, zoom int) (x, y int) {
	n := math.Pow(2, float64(zoom))
	x = int(math.Floor((lon + 180.0) / 360.0 * n))
	latRad := lat * math.Pi / 180.0
	y = int(math.Floor((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n))
	return
}

// latLonToPixel converts lat/lon to global pixel coordinates at the given zoom.
func latLonToPixel(lat, lon float64, zoom int) (px, py float64) {
	n := math.Pow(2, float64(zoom))
	px = (lon + 180.0) / 360.0 * n * 256
	latRad := lat * math.Pi / 180.0
	py = (1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n * 256
	return
}

// pickZoom selects an appropriate zoom level for the bounding box.
func pickZoom(minLat, maxLat, minLon, maxLon float64, widthPx int) int {
	latSpan := maxLat - minLat
	lonSpan := maxLon - minLon
	span := math.Max(latSpan, lonSpan)

	// Approximate: at zoom 0, the world is 360°. Each zoom doubles resolution.
	for z := 18; z >= 1; z-- {
		degreesPerTile := 360.0 / math.Pow(2, float64(z))
		tilesNeeded := span / degreesPerTile
		if tilesNeeded <= 5 {
			return z
		}
	}
	return 1
}

// scaleImage performs nearest-neighbour scaling of an image to the target size.
func scaleImage(src image.Image, dstW, dstH int) image.Image {
	srcBounds := src.Bounds()
	srcW := srcBounds.Dx()
	srcH := srcBounds.Dy()

	if srcW == 0 || srcH == 0 {
		return nil
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := srcBounds.Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			srcX := srcBounds.Min.X + x*srcW/dstW
			dst.Set(x, y, src.At(srcX, srcY))
		}
	}
	return dst
}
