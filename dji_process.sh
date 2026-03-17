#!/usr/bin/env bash
# =============================================================================
# dji_process.sh — Automated DJI Drone Video Post-Processing Pipeline
# =============================================================================
set -euo pipefail

VERSION="1.0.0"
SCRIPT_NAME="$(basename "$0")"

# -----------------------------------------------------------------------------
# Color helpers
# -----------------------------------------------------------------------------
RED='\033[0;31m'; YELLOW='\033[1;33m'; GREEN='\033[0;32m'
CYAN='\033[0;36m'; BOLD='\033[1m'; RESET='\033[0m'

log_info()    { printf "${CYAN}[INFO]${RESET}  %s\n" "$*"; }
log_ok()      { printf "${GREEN}[OK]${RESET}    %s\n" "$*"; }
log_warn()    { printf "${YELLOW}[WARN]${RESET}  %s\n" "$*"; }
log_error()   { printf "${RED}[ERROR]${RESET} %s\n" "$*" >&2; }
log_verbose() { [[ "${VERBOSE}" == "true" ]] && printf "${CYAN}[VERB]${RESET}  %s\n" "$*" || true; }
log_bold()    { printf "${BOLD}%s${RESET}\n" "$*"; }

# -----------------------------------------------------------------------------
# Defaults
# -----------------------------------------------------------------------------
INPUT_DIR=""
OUTPUT_DIR=""
BITRATE="15M"
CODEC="h264"
PRESET="medium"
CRF=""
SKIP_VIDEO="false"
SKIP_TELEMETRY="false"
SKIP_PDF="false"
SKIP_CHARTS="false"
FORCE="false"
RECURSIVE="false"
VERBOSE="false"
DRY_RUN="false"
OPERATOR="Unknown"
FLIGHT_NUM=1

# -----------------------------------------------------------------------------
# Usage / help
# -----------------------------------------------------------------------------
usage() {
cat <<EOF
${BOLD}${SCRIPT_NAME}${RESET} v${VERSION} — DJI Drone Video Post-Processing Pipeline

${BOLD}USAGE${RESET}
  ${SCRIPT_NAME} -i <dir> [options]

${BOLD}REQUIRED${RESET}
  -i, --input   DIR       Input directory containing .MP4 / .mp4 files

${BOLD}OPTIONS${RESET}
  -o, --output  DIR       Output directory (default: <input_dir>/processed)
  -b, --bitrate RATE      Video bitrate (default: 15M)
  -c, --codec   h264|h265 Video codec (default: h264)
  -p, --preset  fast|medium|slow  FFmpeg preset (default: medium)
  -q, --crf     VALUE     Constant rate factor (optional, overrides bitrate)
      --skip-video        Skip video transcoding
      --skip-telemetry    Skip telemetry extraction (GPX, charts)
      --skip-pdf          Skip PDF generation
      --skip-charts       Skip chart generation
  -f, --force             Overwrite existing output files
  -r, --recursive         Recurse into subdirectories
  -v, --verbose           Verbose output
      --dry-run           Show what would be done, do nothing
      --operator NAME     Operator name for PDF (default: Unknown)
      --version           Print version and exit
  -h, --help              Show this help

${BOLD}EXAMPLES${RESET}
  ${SCRIPT_NAME} -i ~/Videos/DJI -o ~/Videos/processed -b 20M -c h264 -p fast
  ${SCRIPT_NAME} -i ./footage --skip-pdf --verbose --dry-run
  ${SCRIPT_NAME} -i ./footage --operator "John Doe" --recursive

EOF
}

# -----------------------------------------------------------------------------
# Argument parsing
# -----------------------------------------------------------------------------
parse_args() {
    while [[ $# -gt 0 ]]; do
        case "$1" in
            -i|--input)      INPUT_DIR="$2";    shift 2 ;;
            -o|--output)     OUTPUT_DIR="$2";   shift 2 ;;
            -b|--bitrate)    BITRATE="$2";      shift 2 ;;
            -c|--codec)      CODEC="$2";        shift 2 ;;
            -p|--preset)     PRESET="$2";       shift 2 ;;
            -q|--crf)        CRF="$2";          shift 2 ;;
            --skip-video)    SKIP_VIDEO="true";       shift ;;
            --skip-telemetry) SKIP_TELEMETRY="true";  shift ;;
            --skip-pdf)      SKIP_PDF="true";         shift ;;
            --skip-charts)   SKIP_CHARTS="true";      shift ;;
            -f|--force)      FORCE="true";            shift ;;
            -r|--recursive)  RECURSIVE="true";        shift ;;
            -v|--verbose)    VERBOSE="true";          shift ;;
            --dry-run)       DRY_RUN="true";          shift ;;
            --operator)      OPERATOR="$2";     shift 2 ;;
            --version)       echo "${SCRIPT_NAME} v${VERSION}"; exit 0 ;;
            -h|--help)       usage; exit 0 ;;
            *) log_error "Unknown argument: $1"; usage; exit 1 ;;
        esac
    done
}

# -----------------------------------------------------------------------------
# Validate arguments
# -----------------------------------------------------------------------------
validate_args() {
    if [[ -z "${INPUT_DIR}" ]]; then
        log_error "Input directory is required (-i / --input)"
        usage; exit 1
    fi
    if [[ ! -d "${INPUT_DIR}" ]]; then
        log_error "Input directory does not exist: ${INPUT_DIR}"
        exit 1
    fi
    if [[ -z "${OUTPUT_DIR}" ]]; then
        OUTPUT_DIR="${INPUT_DIR}/processed"
    fi
    case "${CODEC}" in
        h264|h265) ;;
        *) log_error "Invalid codec '${CODEC}'. Must be h264 or h265."; exit 1 ;;
    esac
    case "${PRESET}" in
        fast|medium|slow|veryfast|faster|veryslow|placebo) ;;
        *) log_error "Invalid preset '${PRESET}'."; exit 1 ;;
    esac
}

# -----------------------------------------------------------------------------
# Dependency checks
# -----------------------------------------------------------------------------
check_dependencies() {
    log_info "Checking system dependencies..."
    local missing=()

    for cmd in ffmpeg exiftool python3; do
        if command -v "${cmd}" &>/dev/null; then
            log_verbose "  Found: ${cmd} ($(command -v "${cmd}"))"
        else
            missing+=("${cmd}")
        fi
    done

    if [[ ${#missing[@]} -gt 0 ]]; then
        log_error "Missing required tools: ${missing[*]}"
        log_error "Install with: brew install ffmpeg exiftool python3"
        exit 1
    fi

    # Check/install Python deps
    log_info "Checking Python dependencies..."
    local py_deps=("matplotlib" "reportlab" "numpy")
    local py_missing=()

    for dep in "${py_deps[@]}"; do
        if python3 -c "import ${dep}" 2>/dev/null; then
            log_verbose "  Python dep found: ${dep}"
        else
            py_missing+=("${dep}")
        fi
    done

    if [[ ${#py_missing[@]} -gt 0 ]]; then
        log_warn "Missing Python packages: ${py_missing[*]}"
        log_info "Auto-installing via pip..."
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_info "[DRY-RUN] Would run: pip3 install ${py_missing[*]}"
        else
            python3 -m pip install --quiet "${py_missing[@]}" || {
                log_error "Failed to install Python packages. Try: pip3 install ${py_missing[*]}"
                exit 1
            }
            log_ok "Python packages installed."
        fi
    fi
}

# -----------------------------------------------------------------------------
# Write embedded Python processor to /tmp
# -----------------------------------------------------------------------------
PYTHON_SCRIPT="/tmp/dji_processor_$$.py"

write_python_script() {
    log_verbose "Writing Python processor to ${PYTHON_SCRIPT}"
    cat > "${PYTHON_SCRIPT}" << 'PYEOF'
#!/usr/bin/env python3
"""
DJI Drone Video Post-Processor
Handles: telemetry extraction, GPX, altitude chart, GPS map, PDF briefing, markdown report
"""
import argparse
import subprocess
import json
import re
import math
import os
import sys
import shutil
import traceback
from datetime import datetime, timezone

# ---------------------------------------------------------------------------
# Optional heavy imports — graceful degradation
# ---------------------------------------------------------------------------
try:
    import numpy as np
    HAS_NUMPY = True
except ImportError:
    HAS_NUMPY = False

try:
    import matplotlib
    matplotlib.use("Agg")
    import matplotlib.pyplot as plt
    import matplotlib.colors as mcolors
    from matplotlib.collections import LineCollection
    from matplotlib.patches import FancyArrowPatch
    HAS_MATPLOTLIB = True
except ImportError:
    HAS_MATPLOTLIB = False

try:
    from reportlab.lib.pagesizes import A4
    from reportlab.lib.units import mm, cm
    from reportlab.lib import colors as rl_colors
    from reportlab.lib.styles import getSampleStyleSheet, ParagraphStyle
    from reportlab.lib.enums import TA_CENTER, TA_LEFT, TA_RIGHT
    from reportlab.platypus import (
        SimpleDocTemplate, Paragraph, Spacer, Table, TableStyle,
        Image as RLImage, PageBreak, HRFlowable, KeepTogether
    )
    from reportlab.platypus.flowables import Flowable
    from reportlab.graphics.shapes import Drawing, Rect, String, Line
    HAS_REPORTLAB = True
except ImportError:
    HAS_REPORTLAB = False

# ---------------------------------------------------------------------------
# Argument parsing
# ---------------------------------------------------------------------------
def parse_args():
    p = argparse.ArgumentParser(description="DJI post-processor")
    p.add_argument("--video",       required=True,  help="Path to input video")
    p.add_argument("--output-dir",  required=True,  help="Output directory")
    p.add_argument("--basename",    required=True,  help="Base name for outputs")
    p.add_argument("--skip-charts", action="store_true")
    p.add_argument("--skip-pdf",    action="store_true")
    p.add_argument("--skip-gpx",    action="store_true")
    p.add_argument("--skip-md",     action="store_true")
    p.add_argument("--operator",    default="Unknown")
    p.add_argument("--flight-num",  type=int, default=1)
    p.add_argument("--verbose",     action="store_true")
    return p.parse_args()

# ---------------------------------------------------------------------------
# Telemetry extraction
# ---------------------------------------------------------------------------
EXIF_FIELDS = (
    "$SampleTime,$GPSDateTime,$GPSLatitude,$GPSLongitude,"
    "$AbsoluteAltitude,$RelativeAltitude,$DroneRoll,$DronePitch,$DroneYaw,"
    "$GimbalPitch,$GimbalYaw,$ISO,$ShutterSpeed,$FNumber,$ColorTemperature"
)
FIELD_NAMES = [
    "SampleTime", "GPSDateTime", "GPSLatitude", "GPSLongitude",
    "AbsoluteAltitude", "RelativeAltitude", "DroneRoll", "DronePitch", "DroneYaw",
    "GimbalPitch", "GimbalYaw", "ISO", "ShutterSpeed", "FNumber", "ColorTemperature"
]

def parse_time(s):
    """Parse exiftool SampleTime: '0 s', '0.03 s', '1:30', '0:03:12'"""
    if s is None:
        return None
    s = s.strip()
    # "X s" or "X.Y s"
    m = re.match(r'^([\d.]+)\s*s$', s)
    if m:
        return float(m.group(1))
    # colon-separated
    parts = s.split(":")
    if len(parts) == 2:
        # MM:SS
        try:
            return int(parts[0]) * 60 + float(parts[1])
        except ValueError:
            pass
    if len(parts) == 3:
        # H:MM:SS
        try:
            return int(parts[0]) * 3600 + int(parts[1]) * 60 + float(parts[2])
        except ValueError:
            pass
    try:
        return float(s)
    except ValueError:
        return None

def parse_dms(s):
    """Parse '57 deg 9' 56.67" N' -> decimal degrees"""
    if s is None:
        return None
    s = s.strip()
    m = re.match(
        r"(\d+)\s*deg\s+(\d+)'\s*([\d.]+)\"\s*([NSEW])", s, re.IGNORECASE
    )
    if m:
        deg = float(m.group(1))
        mn  = float(m.group(2))
        sec = float(m.group(3))
        ref = m.group(4).upper()
        dec = deg + mn / 60.0 + sec / 3600.0
        if ref in ("S", "W"):
            dec = -dec
        return dec
    try:
        return float(s)
    except ValueError:
        return None

def parse_float(s):
    if s is None:
        return None
    try:
        return float(str(s).strip())
    except ValueError:
        return None

def extract_telemetry(video_path, verbose=False):
    """Run exiftool and parse output into list of dicts."""
    cmd = [
        "exiftool", "-ee",
        "-p", EXIF_FIELDS,
        video_path
    ]
    if verbose:
        print(f"[VERB] exiftool cmd: {' '.join(cmd)}", flush=True)

    try:
        result = subprocess.run(
            cmd, capture_output=True, text=True, timeout=300
        )
    except subprocess.TimeoutExpired:
        print("[ERROR] exiftool timed out", flush=True)
        return []
    except FileNotFoundError:
        print("[ERROR] exiftool not found", flush=True)
        return []

    if result.returncode != 0 and not result.stdout.strip():
        if verbose:
            print(f"[VERB] exiftool stderr: {result.stderr[:500]}", flush=True)
        return []

    rows = []
    for line in result.stdout.splitlines():
        line = line.strip()
        if not line:
            continue
        parts = line.split(",")
        # pad / truncate to FIELD_NAMES length
        while len(parts) < len(FIELD_NAMES):
            parts.append("")
        row = {}
        for i, name in enumerate(FIELD_NAMES):
            row[name] = parts[i].strip() if i < len(parts) else ""
        rows.append(row)

    if verbose:
        print(f"[VERB] Extracted {len(rows)} telemetry rows", flush=True)
    return rows

def parse_telemetry(rows, verbose=False):
    """Convert raw rows to typed records."""
    records = []
    for row in rows:
        try:
            t   = parse_time(row.get("SampleTime") or "")
            lat = parse_dms(row.get("GPSLatitude") or "")
            lon = parse_dms(row.get("GPSLongitude") or "")
            alt_asl = parse_float(row.get("AbsoluteAltitude"))
            alt_agl = parse_float(row.get("RelativeAltitude"))
            roll    = parse_float(row.get("DroneRoll"))
            pitch   = parse_float(row.get("DronePitch"))
            yaw     = parse_float(row.get("DroneYaw"))
            g_pitch = parse_float(row.get("GimbalPitch"))
            g_yaw   = parse_float(row.get("GimbalYaw"))
            iso     = row.get("ISO", "")
            ss      = row.get("ShutterSpeed", "")
            fnumber = parse_float(row.get("FNumber"))
            color_temp = row.get("ColorTemperature", "")
            gps_dt  = row.get("GPSDateTime", "")

            records.append({
                "t": t, "lat": lat, "lon": lon,
                "alt_asl": alt_asl, "alt_agl": alt_agl,
                "roll": roll, "pitch": pitch, "yaw": yaw,
                "gimbal_pitch": g_pitch, "gimbal_yaw": g_yaw,
                "iso": iso, "shutter": ss, "fnumber": fnumber,
                "color_temp": color_temp, "gps_dt": gps_dt,
            })
        except Exception as e:
            if verbose:
                print(f"[VERB] Row parse error: {e}", flush=True)
            continue
    return records

def filter_gps(records):
    """Return records that have valid GPS coordinates."""
    return [r for r in records if r["lat"] is not None and r["lon"] is not None]

# ---------------------------------------------------------------------------
# GPX generation
# ---------------------------------------------------------------------------
def format_gpx_time(gps_dt_str, fallback_dt=None):
    """Try to format a GPSDateTime string as ISO 8601 UTC."""
    if gps_dt_str:
        # exiftool format: "2026:03:17 08:27:16.000"
        m = re.match(r"(\d{4}):(\d{2}):(\d{2})\s+(\d{2}):(\d{2}):([\d.]+)", gps_dt_str)
        if m:
            yr, mo, dy, hr, mi, se = m.groups()
            se_f = float(se)
            ms = int((se_f % 1) * 1000)
            se_i = int(se_f)
            return f"{yr}-{mo}-{dy}T{hr}:{mi}:{se_i:02d}.{ms:03d}Z"
    if fallback_dt:
        return fallback_dt.strftime("%Y-%m-%dT%H:%M:%S.000Z")
    return datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")

def write_gpx(records, output_path, basename, verbose=False):
    """Downsample to ~1Hz (every 30th row, always include last) and write GPX."""
    gps_records = filter_gps(records)
    if not gps_records:
        print("[WARN] No GPS data found, skipping GPX.", flush=True)
        return False

    step = 30
    indices = list(range(0, len(gps_records), step))
    if (len(gps_records) - 1) not in indices:
        indices.append(len(gps_records) - 1)
    sampled = [gps_records[i] for i in indices]

    lines = [
        '<?xml version="1.0" encoding="UTF-8"?>',
        '<gpx version="1.1" creator="dji_process.sh"',
        '     xmlns="http://www.topografix.com/GPX/1/1"',
        '     xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"',
        '     xsi:schemaLocation="http://www.topografix.com/GPX/1/1',
        '       http://www.topografix.com/GPX/1/1/gpx.xsd">',
        f'  <metadata><name>{basename}</name></metadata>',
        '  <trk>',
        f'    <name>{basename}</name>',
        '    <trkseg>',
    ]

    now_str = datetime.now(timezone.utc).strftime("%Y-%m-%dT%H:%M:%S.000Z")
    for r in sampled:
        lat = f"{r['lat']:.8f}"
        lon = f"{r['lon']:.8f}"
        ele = f"{r['alt_asl']:.3f}" if r['alt_asl'] is not None else "0.000"
        t_str = format_gpx_time(r.get("gps_dt", ""), None) or now_str

        roll  = r.get("roll")  or 0.0
        pitch = r.get("pitch") or 0.0
        yaw   = r.get("yaw")   or 0.0
        agl   = r.get("alt_agl") or 0.0
        iso   = r.get("iso", "")
        ss    = r.get("shutter", "")
        desc = (f"AGL:{agl:.1f}m Roll:{roll:.1f} Pitch:{pitch:.1f} "
                f"Yaw:{yaw:.1f} ISO:{iso} SS:{ss}")

        lines.append(f'      <trkpt lat="{lat}" lon="{lon}">')
        lines.append(f'        <ele>{ele}</ele>')
        lines.append(f'        <time>{t_str}</time>')
        lines.append(f'        <desc>{desc}</desc>')
        lines.append('      </trkpt>')

    lines += ['    </trkseg>', '  </trk>', '</gpx>']

    with open(output_path, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")

    if verbose:
        print(f"[VERB] GPX written: {output_path} ({len(sampled)} points)", flush=True)
    return True

# ---------------------------------------------------------------------------
# Altitude chart
# ---------------------------------------------------------------------------
def format_mmss(seconds):
    if seconds is None:
        return "0:00"
    seconds = int(seconds)
    m = seconds // 60
    s = seconds % 60
    return f"{m}:{s:02d}"

def make_altitude_chart(records, output_path, basename, verbose=False):
    """Two-panel altitude chart (ASL top, AGL bottom), light theme."""
    if not HAS_MATPLOTLIB:
        print("[WARN] matplotlib not available, skipping altitude chart.", flush=True)
        return False

    valid = [r for r in records if r.get("t") is not None
             and r.get("alt_asl") is not None
             and r.get("alt_agl") is not None]
    if len(valid) < 2:
        print("[WARN] Not enough altitude data for chart.", flush=True)
        return False

    times   = [r["t"]       for r in valid]
    alt_asl = [r["alt_asl"] for r in valid]
    alt_agl = [r["alt_agl"] for r in valid]

    # x-tick positions every 30 seconds
    max_t = max(times)
    tick_interval = 30
    tick_positions = list(range(0, int(max_t) + tick_interval, tick_interval))
    tick_labels = [format_mmss(t) for t in tick_positions]

    fig, (ax1, ax2) = plt.subplots(2, 1, figsize=(12, 6), sharex=True,
                                    facecolor="white")
    fig.patch.set_facecolor("white")

    # --- Top: ASL ---
    ax1.set_facecolor("white")
    ax1.plot(times, alt_asl, color="#1565C0", linewidth=1.5, zorder=3)
    ax1.fill_between(times, alt_asl, alpha=0.25, color="#1565C0", zorder=2)
    ax1.set_ylabel("Altitude ASL (m)", fontsize=9, color="#333333")
    ax1.tick_params(axis="y", labelsize=8, colors="#333333")
    ax1.grid(True, linestyle="--", linewidth=0.5, color="#cccccc", zorder=1)
    ax1.spines["top"].set_visible(False)
    ax1.spines["right"].set_visible(False)

    max_asl = max(alt_asl)
    min_asl = min(alt_asl)
    max_t_asl = times[alt_asl.index(max_asl)]
    min_t_asl = times[alt_asl.index(min_asl)]
    ax1.annotate(f"Max: {max_asl:.1f}m",
                 xy=(max_t_asl, max_asl), xytext=(5, 5),
                 textcoords="offset points", fontsize=7, color="#1565C0")
    ax1.annotate(f"Min: {min_asl:.1f}m",
                 xy=(min_t_asl, min_asl), xytext=(5, -12),
                 textcoords="offset points", fontsize=7, color="#1565C0")
    ax1.set_title(f"{basename} — Altitude Profile", fontsize=11,
                  color="#0d2137", fontweight="bold", pad=8)

    # --- Bottom: AGL ---
    ax2.set_facecolor("white")
    ax2.plot(times, alt_agl, color="#2E7D32", linewidth=1.5, zorder=3)
    ax2.fill_between(times, 0, alt_agl, alpha=0.25, color="#2E7D32", zorder=2)
    ax2.axhline(0, color="#888888", linewidth=0.8, linestyle="-")
    ax2.set_ylabel("Altitude AGL (m)", fontsize=9, color="#333333")
    ax2.set_xlabel("Time (MM:SS)", fontsize=9, color="#333333")
    ax2.tick_params(axis="both", labelsize=8, colors="#333333")
    ax2.grid(True, linestyle="--", linewidth=0.5, color="#cccccc", zorder=1)
    ax2.spines["top"].set_visible(False)
    ax2.spines["right"].set_visible(False)

    max_agl = max(alt_agl)
    min_agl = min(alt_agl)
    max_t_agl = times[alt_agl.index(max_agl)]
    min_t_agl = times[alt_agl.index(min_agl)]
    ax2.annotate(f"Max: {max_agl:.1f}m",
                 xy=(max_t_agl, max_agl), xytext=(5, 5),
                 textcoords="offset points", fontsize=7, color="#2E7D32")
    ax2.annotate(f"Min: {min_agl:.1f}m",
                 xy=(min_t_agl, min_agl), xytext=(5, -12),
                 textcoords="offset points", fontsize=7, color="#2E7D32")

    ax2.set_xticks(tick_positions)
    ax2.set_xticklabels(tick_labels, rotation=45, ha="right", fontsize=7)
    ax2.set_xlim(0, max_t)

    plt.tight_layout(pad=1.5)
    fig.savefig(output_path, dpi=150, bbox_inches="tight",
                facecolor="white", edgecolor="none")
    plt.close(fig)

    if verbose:
        print(f"[VERB] Altitude chart saved: {output_path}", flush=True)
    return True

# ---------------------------------------------------------------------------
# GPS track map
# ---------------------------------------------------------------------------
def make_gps_map(records, output_path, basename, verbose=False):
    """GPS track map colored by AGL altitude."""
    if not HAS_MATPLOTLIB:
        print("[WARN] matplotlib not available, skipping GPS map.", flush=True)
        return False

    gps = filter_gps(records)
    if len(gps) < 2:
        print("[WARN] Not enough GPS data for map.", flush=True)
        return False

    lats = [r["lat"] for r in gps]
    lons = [r["lon"] for r in gps]
    agls = [r["alt_agl"] if r["alt_agl"] is not None else 0.0 for r in gps]

    center_lat = (min(lats) + max(lats)) / 2.0
    aspect = 1.0 / math.cos(math.radians(center_lat))

    fig, ax = plt.subplots(figsize=(8, 8), facecolor="#e8e8e8")
    ax.set_facecolor("#d8d8d8")

    # Build LineCollection segments colored by AGL
    points = list(zip(lons, lats))
    segments = [
        [points[i], points[i + 1]]
        for i in range(len(points) - 1)
    ]

    if HAS_NUMPY:
        agl_arr = np.array(agls)
    else:
        agl_arr = agls

    norm_min = min(agls)
    norm_max = max(agls) if max(agls) != min(agls) else norm_min + 1.0

    norm = mcolors.Normalize(vmin=norm_min, vmax=norm_max)
    cmap = plt.get_cmap("RdYlGn")

    # Segment colors from mid-point AGL
    seg_colors = [cmap(norm((agls[i] + agls[i + 1]) / 2.0))
                  for i in range(len(agls) - 1)]

    lc = LineCollection(segments, colors=seg_colors, linewidth=2.5, zorder=3)
    ax.add_collection(lc)

    # Colorbar
    sm = plt.cm.ScalarMappable(cmap=cmap, norm=norm)
    sm.set_array([])
    cbar = fig.colorbar(sm, ax=ax, fraction=0.035, pad=0.02)
    cbar.set_label("Altitude AGL (m)", fontsize=9, color="#333333")
    cbar.ax.tick_params(labelsize=8, colors="#333333")

    # Start marker (green triangle)
    ax.plot(lons[0], lats[0], marker="^", color="#00C853",
            markersize=12, zorder=5, label="Start")
    ax.annotate("▲ Start", xy=(lons[0], lats[0]),
                xytext=(4, 4), textcoords="offset points",
                fontsize=7, color="#00C853", fontweight="bold", zorder=6)

    # End marker (red square)
    ax.plot(lons[-1], lats[-1], marker="s", color="#D50000",
            markersize=10, zorder=5, label="End")
    ax.annotate("■ End", xy=(lons[-1], lats[-1]),
                xytext=(4, 4), textcoords="offset points",
                fontsize=7, color="#D50000", fontweight="bold", zorder=6)

    ax.set_aspect(aspect)
    ax.set_xlabel("Longitude", fontsize=9, color="#333333")
    ax.set_ylabel("Latitude",  fontsize=9, color="#333333")
    ax.tick_params(axis="both", labelsize=7, colors="#333333")
    ax.grid(True, linestyle="--", linewidth=0.4, color="#bbbbbb", zorder=1)

    # Padding
    lat_range = max(lats) - min(lats) or 0.001
    lon_range = max(lons) - min(lons) or 0.001
    pad_lat = lat_range * 0.15
    pad_lon = lon_range * 0.15
    ax.set_xlim(min(lons) - pad_lon, max(lons) + pad_lon)
    ax.set_ylim(min(lats) - pad_lat, max(lats) + pad_lat)

    # North arrow
    xlim = ax.get_xlim()
    ylim = ax.get_ylim()
    arrow_x = xlim[0] + (xlim[1] - xlim[0]) * 0.93
    arrow_y = ylim[0] + (ylim[1] - ylim[0]) * 0.08
    arrow_len = (ylim[1] - ylim[0]) * 0.07
    ax.annotate("", xy=(arrow_x, arrow_y + arrow_len),
                xytext=(arrow_x, arrow_y),
                arrowprops=dict(arrowstyle="->", color="#333333", lw=1.5),
                zorder=7)
    ax.text(arrow_x, arrow_y + arrow_len * 1.15, "N",
            ha="center", va="bottom", fontsize=9,
            fontweight="bold", color="#333333", zorder=7)

    # Scale bar (approximate)
    deg_per_m_lat = 1.0 / 111320.0
    bar_meters = 100
    bar_deg = bar_meters * deg_per_m_lat
    bar_x0 = xlim[0] + (xlim[1] - xlim[0]) * 0.05
    bar_y0 = ylim[0] + (ylim[1] - ylim[0]) * 0.04
    ax.plot([bar_x0, bar_x0 + bar_deg], [bar_y0, bar_y0],
            color="#333333", linewidth=2, zorder=7)
    ax.text(bar_x0 + bar_deg / 2, bar_y0 + lat_range * 0.02,
            f"{bar_meters} m", ha="center", va="bottom",
            fontsize=7, color="#333333", zorder=7)

    ax.set_title(f"{basename} — GPS Track", fontsize=11,
                 color="#0d2137", fontweight="bold", pad=8)
    ax.legend(loc="upper left", fontsize=7, framealpha=0.8)

    plt.tight_layout(pad=1.5)
    fig.savefig(output_path, dpi=150, bbox_inches="tight",
                facecolor="#e8e8e8", edgecolor="none")
    plt.close(fig)

    if verbose:
        print(f"[VERB] GPS map saved: {output_path}", flush=True)
    return True

# ---------------------------------------------------------------------------
# Flight statistics
# ---------------------------------------------------------------------------
def compute_stats(records):
    """Compute flight statistics from telemetry records."""
    gps = filter_gps(records)
    stats = {}

    if records:
        times = [r["t"] for r in records if r.get("t") is not None]
        if times:
            stats["duration_s"] = max(times)
            dur = int(max(times))
            stats["duration_str"] = f"{dur // 3600}h {(dur % 3600) // 60}m {dur % 60}s"

    agls = [r["alt_agl"] for r in records if r.get("alt_agl") is not None]
    asls = [r["alt_asl"] for r in records if r.get("alt_asl") is not None]
    if agls:
        stats["max_agl"] = max(agls)
        stats["min_agl"] = min(agls)
    if asls:
        stats["max_asl"] = max(asls)
        stats["min_asl"] = min(asls)

    if len(gps) >= 2:
        stats["gps_points"] = len(gps)
        stats["start_lat"] = gps[0]["lat"]
        stats["start_lon"] = gps[0]["lon"]
        stats["end_lat"]   = gps[-1]["lat"]
        stats["end_lon"]   = gps[-1]["lon"]
        # Total distance (haversine)
        total_dist = 0.0
        R = 6371000.0
        for i in range(len(gps) - 1):
            la1, lo1 = math.radians(gps[i]["lat"]),  math.radians(gps[i]["lon"])
            la2, lo2 = math.radians(gps[i+1]["lat"]), math.radians(gps[i+1]["lon"])
            dlat = la2 - la1
            dlon = lo2 - lo1
            a = math.sin(dlat/2)**2 + math.cos(la1)*math.cos(la2)*math.sin(dlon/2)**2
            total_dist += 2 * R * math.asin(math.sqrt(a))
        stats["total_distance_m"] = total_dist

    # Camera — use first record with iso
    for r in records:
        if r.get("iso"):
            stats["iso"] = r["iso"]
            stats["shutter"] = r.get("shutter", "")
            stats["fnumber"] = r.get("fnumber")
            stats["color_temp"] = r.get("color_temp", "")
            break

    return stats

# ---------------------------------------------------------------------------
# Markdown report
# ---------------------------------------------------------------------------
def write_markdown(records, stats, output_path, basename, operator_name,
                   flight_num, video_path, verbose=False):
    now = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    dur = stats.get("duration_str", "N/A")
    max_agl = stats.get("max_agl", "N/A")
    max_asl = stats.get("max_asl", "N/A")
    dist = stats.get("total_distance_m")
    dist_str = f"{dist:.0f} m" if dist is not None else "N/A"

    lines = [
        f"# Post-Flight Telemetry Report",
        f"",
        f"**File:** `{os.path.basename(video_path)}`  ",
        f"**Generated:** {now}  ",
        f"**Operator:** {operator_name}  ",
        f"**Flight #:** {flight_num}  ",
        f"",
        f"## Flight Statistics",
        f"",
        f"| Parameter | Value |",
        f"|-----------|-------|",
        f"| Duration | {dur} |",
        f"| Max Altitude AGL | {max_agl if isinstance(max_agl, str) else f'{max_agl:.1f} m'} |",
        f"| Max Altitude ASL | {max_asl if isinstance(max_asl, str) else f'{max_asl:.1f} m'} |",
        f"| Total Distance | {dist_str} |",
        f"| GPS Points | {stats.get('gps_points', 'N/A')} |",
        f"",
        f"## Camera Settings (first record)",
        f"",
        f"| Parameter | Value |",
        f"|-----------|-------|",
        f"| ISO | {stats.get('iso', 'N/A')} |",
        f"| Shutter Speed | {stats.get('shutter', 'N/A')} |",
        f"| Aperture | f/{stats.get('fnumber', 'N/A')} |",
        f"| Color Temperature | {stats.get('color_temp', 'N/A')} |",
        f"",
        f"## Sample Telemetry (first 10 GPS records)",
        f"",
        f"| Time | Lat | Lon | Alt ASL | Alt AGL | Roll | Pitch | Yaw |",
        f"|------|-----|-----|---------|---------|------|-------|-----|",
    ]

    gps = filter_gps(records)
    for r in gps[:10]:
        t_str = format_mmss(r.get("t"))
        lat   = f"{r['lat']:.6f}" if r['lat'] is not None else "N/A"
        lon   = f"{r['lon']:.6f}" if r['lon'] is not None else "N/A"
        asl   = f"{r['alt_asl']:.1f}" if r.get('alt_asl') is not None else "N/A"
        agl   = f"{r['alt_agl']:.1f}" if r.get('alt_agl') is not None else "N/A"
        roll  = f"{r.get('roll', 0.0):.1f}" if r.get('roll') is not None else "N/A"
        pitch = f"{r.get('pitch', 0.0):.1f}" if r.get('pitch') is not None else "N/A"
        yaw   = f"{r.get('yaw', 0.0):.1f}" if r.get('yaw') is not None else "N/A"
        lines.append(f"| {t_str} | {lat} | {lon} | {asl} | {agl} | {roll} | {pitch} | {yaw} |")

    with open(output_path, "w", encoding="utf-8") as f:
        f.write("\n".join(lines) + "\n")

    if verbose:
        print(f"[VERB] Markdown report: {output_path}", flush=True)
    return True

# ---------------------------------------------------------------------------
# PDF generation
# ---------------------------------------------------------------------------
def format_float(v, fmt=".1f", fallback="N/A"):
    if v is None:
        return fallback
    try:
        return format(float(v), fmt)
    except (TypeError, ValueError):
        return fallback

def make_pdf(records, stats, output_path, basename, operator_name,
             flight_num, video_path, map_img, alt_img, verbose=False):
    if not HAS_REPORTLAB:
        print("[WARN] reportlab not available, skipping PDF.", flush=True)
        return False

    # Colors
    navy   = rl_colors.HexColor("#0d2137")
    amber  = rl_colors.HexColor("#e8a020")
    light  = rl_colors.HexColor("#f5f7fa")
    white  = rl_colors.white
    dgray  = rl_colors.HexColor("#333333")
    lgray  = rl_colors.HexColor("#cccccc")
    mgray  = rl_colors.HexColor("#888888")

    doc = SimpleDocTemplate(
        output_path,
        pagesize=A4,
        leftMargin=15*mm, rightMargin=15*mm,
        topMargin=12*mm,  bottomMargin=18*mm,
        title=f"Post-Flight Briefing — {basename}",
        author=operator_name,
    )

    PAGE_W, PAGE_H = A4
    CONTENT_W = PAGE_W - 30*mm
    now_str = datetime.now().strftime("%Y-%m-%d %H:%M:%S")

    styles = getSampleStyleSheet()

    # Custom styles
    style_title = ParagraphStyle(
        "BriefTitle",
        fontName="Helvetica-Bold",
        fontSize=18,
        textColor=white,
        alignment=TA_LEFT,
        leading=22,
    )
    style_sub = ParagraphStyle(
        "BriefSub",
        fontName="Helvetica",
        fontSize=9,
        textColor=rl_colors.HexColor("#aac0d8"),
        leading=12,
    )
    style_section = ParagraphStyle(
        "Section",
        fontName="Helvetica-Bold",
        fontSize=9,
        textColor=amber,
        spaceBefore=4,
        spaceAfter=2,
        leading=11,
    )
    style_normal = ParagraphStyle(
        "Normal2",
        fontName="Helvetica",
        fontSize=8,
        textColor=dgray,
        leading=10,
    )
    style_footer = ParagraphStyle(
        "Footer",
        fontName="Helvetica",
        fontSize=7,
        textColor=mgray,
        alignment=TA_CENTER,
    )

    # --------------- Header flowable ---------------
    class HeaderBand(Flowable):
        def __init__(self, width, basename, operator, flight_num, date_str):
            super().__init__()
            self.width = width
            self.basename = basename
            self.operator = operator
            self.flight_num = flight_num
            self.date_str = date_str
            self.height = 38*mm

        def draw(self):
            c = self.canv
            # Navy background
            c.setFillColor(navy)
            c.rect(0, 0, self.width, self.height, stroke=0, fill=1)
            # Amber accent bar
            c.setFillColor(amber)
            c.rect(0, self.height - 2.5*mm, self.width, 2.5*mm, stroke=0, fill=1)
            # Title
            c.setFillColor(white)
            c.setFont("Helvetica-Bold", 20)
            c.drawString(8*mm, self.height - 14*mm, "POST-FLIGHT BRIEFING")
            # Subtitle line
            c.setFont("Helvetica", 9)
            c.setFillColor(rl_colors.HexColor("#aac0d8"))
            c.drawString(8*mm, self.height - 21*mm,
                         f"Aircraft: {self.basename}   |   Operator: {self.operator}"
                         f"   |   Flight #: {self.flight_num}   |   Date: {self.date_str}")
            # UNOFFICIAL stamp
            c.setFillColor(amber)
            c.setFont("Helvetica-Bold", 7)
            c.drawRightString(self.width - 8*mm, 4*mm,
                              "UNOFFICIAL — FOR REFERENCE ONLY")
            c.setFillColor(rl_colors.HexColor("#aac0d8"))
            c.setFont("Helvetica", 7)
            c.drawString(8*mm, 4*mm, f"Generated: {self.date_str}")

    # --------------- Compute stats ---------------
    dur_s = stats.get("duration_s", 0) or 0
    dur_str = stats.get("duration_str", "N/A")
    max_agl = format_float(stats.get("max_agl"))
    min_agl = format_float(stats.get("min_agl"))
    max_asl = format_float(stats.get("max_asl"))
    min_asl = format_float(stats.get("min_asl"))
    dist_m  = stats.get("total_distance_m")
    dist_str = f"{dist_m:.0f} m" if dist_m is not None else "N/A"
    gps_pts = str(stats.get("gps_points", "N/A"))
    start_lat = format_float(stats.get("start_lat"), ".6f")
    start_lon = format_float(stats.get("start_lon"), ".6f")
    end_lat   = format_float(stats.get("end_lat"),   ".6f")
    end_lon   = format_float(stats.get("end_lon"),   ".6f")

    iso_v   = stats.get("iso",       "N/A")
    ss_v    = stats.get("shutter",   "N/A")
    fn_v    = format_float(stats.get("fnumber"), ".1f")
    ct_v    = stats.get("color_temp","N/A")

    flight_date = datetime.now().strftime("%Y-%m-%d")

    # --------------- Table styles ---------------
    tbl_style_base = [
        ("BACKGROUND",  (0, 0), (-1, 0), navy),
        ("TEXTCOLOR",   (0, 0), (-1, 0), white),
        ("FONTNAME",    (0, 0), (-1, 0), "Helvetica-Bold"),
        ("FONTSIZE",    (0, 0), (-1, 0), 7.5),
        ("FONTNAME",    (0, 1), (-1, -1), "Helvetica"),
        ("FONTSIZE",    (0, 1), (-1, -1), 7.5),
        ("ROWBACKGROUNDS", (0, 1), (-1, -1), [light, white]),
        ("GRID",        (0, 0), (-1, -1), 0.4, lgray),
        ("VALIGN",      (0, 0), (-1, -1), "MIDDLE"),
        ("TOPPADDING",  (0, 0), (-1, -1), 2),
        ("BOTTOMPADDING",(0, 0),(-1, -1), 2),
        ("LEFTPADDING", (0, 0), (-1, -1), 4),
        ("RIGHTPADDING",(0, 0), (-1, -1), 4),
    ]

    def make_table(headers, rows, col_widths):
        data = [headers] + rows
        t = Table(data, colWidths=col_widths)
        t.setStyle(TableStyle(tbl_style_base))
        return t

    # Flight stats table
    flight_rows = [
        ["Duration",        dur_str],
        ["Max Alt AGL",     f"{max_agl} m"],
        ["Min Alt AGL",     f"{min_agl} m"],
        ["Max Alt ASL",     f"{max_asl} m"],
        ["Min Alt ASL",     f"{min_asl} m"],
        ["Total Distance",  dist_str],
        ["GPS Points",      gps_pts],
        ["Start Position",  f"{start_lat}, {start_lon}"],
        ["End Position",    f"{end_lat}, {end_lon}"],
        ["Flight Date",     flight_date],
        ["Operator",        operator_name],
        ["File",            os.path.basename(video_path)],
    ]
    flight_tbl = make_table(
        ["Parameter", "Value"],
        flight_rows,
        [45*mm, 55*mm]
    )

    # Camera settings table
    cam_rows = [
        ["ISO",           str(iso_v)],
        ["Shutter Speed", str(ss_v)],
        ["Aperture",      f"f/{fn_v}"],
        ["Color Temp",    str(ct_v)],
    ]
    cam_tbl = make_table(
        ["Camera Parameter", "Value"],
        cam_rows,
        [45*mm, 55*mm]
    )

    # GPS map image — small (side column, page 1) and large (full-width, page 2)
    map_img_rl = None
    map_img_rl_large = None
    if map_img and os.path.isfile(map_img):
        map_img_rl = RLImage(map_img,
                             width=CONTENT_W * 0.45,
                             height=CONTENT_W * 0.45)
        map_img_rl_large = RLImage(map_img,
                                   width=CONTENT_W,
                                   height=CONTENT_W * 0.7)

    # Altitude image
    alt_img_rl = None
    if alt_img and os.path.isfile(alt_img):
        alt_img_rl = RLImage(alt_img,
                             width=CONTENT_W,
                             height=CONTENT_W * 0.35)

    # --------------- Page 1 ---------------
    story = []

    story.append(HeaderBand(CONTENT_W, basename, operator_name, flight_num, now_str))
    story.append(Spacer(1, 4*mm))

    # Two-column layout: left=tables, right=map
    left_w  = CONTENT_W * 0.52
    right_w = CONTENT_W * 0.46
    gap_w   = CONTENT_W * 0.02

    left_col = [
        Paragraph("FLIGHT STATISTICS", style_section),
        Spacer(1, 1*mm),
        flight_tbl,
        Spacer(1, 3*mm),
        Paragraph("CAMERA SETTINGS", style_section),
        Spacer(1, 1*mm),
        cam_tbl,
    ]

    right_col = []
    right_col.append(Paragraph("GPS TRACK", style_section))
    right_col.append(Spacer(1, 1*mm))
    if map_img_rl:
        right_col.append(map_img_rl)
    else:
        right_col.append(Paragraph("(No GPS data available)", style_normal))

    # Render columns via Table
    two_col = Table(
        [[left_col, Spacer(gap_w, 1), right_col]],
        colWidths=[left_w, gap_w, right_w]
    )
    two_col.setStyle(TableStyle([
        ("VALIGN",  (0, 0), (-1, -1), "TOP"),
        ("LEFTPADDING",  (0, 0), (-1, -1), 0),
        ("RIGHTPADDING", (0, 0), (-1, -1), 0),
        ("TOPPADDING",   (0, 0), (-1, -1), 0),
        ("BOTTOMPADDING",(0, 0), (-1, -1), 0),
    ]))
    story.append(two_col)

    # --------------- Page 2 ---------------
    story.append(PageBreak())
    story.append(HeaderBand(CONTENT_W, basename, operator_name, flight_num, now_str))
    story.append(Spacer(1, 4*mm))

    story.append(Paragraph("ALTITUDE PROFILE", style_section))
    story.append(Spacer(1, 1*mm))
    if alt_img_rl:
        story.append(alt_img_rl)
    else:
        story.append(Paragraph("(No altitude data available)", style_normal))

    story.append(Spacer(1, 4*mm))
    story.append(Paragraph("FLIGHT PATH MAP", style_section))
    story.append(Spacer(1, 1*mm))
    if map_img_rl_large:
        story.append(map_img_rl_large)
    else:
        story.append(Paragraph("(No GPS data available)", style_normal))

    story.append(Spacer(1, 4*mm))
    story.append(Paragraph("ATTITUDE & GIMBAL DATA (sample, every 60s)", style_section))
    story.append(Spacer(1, 1*mm))

    # Sample attitude rows every ~60 sec
    gps = filter_gps(records)
    att_rows = []
    last_t = -999
    for r in records:
        t = r.get("t")
        if t is None:
            continue
        if t - last_t >= 60.0 or last_t < 0:
            att_rows.append([
                format_mmss(t),
                format_float(r.get("roll"),        ".1f"),
                format_float(r.get("pitch"),       ".1f"),
                format_float(r.get("yaw"),         ".1f"),
                format_float(r.get("gimbal_pitch"),".1f"),
                format_float(r.get("gimbal_yaw"),  ".1f"),
                format_float(r.get("alt_agl"),     ".1f"),
            ])
            last_t = t

    att_col_w = [20*mm, 25*mm, 25*mm, 25*mm, 28*mm, 28*mm, 24*mm]
    att_tbl = make_table(
        ["Time", "Roll°", "Pitch°", "Yaw°", "Gim.Pitch°", "Gim.Yaw°", "AGL m"],
        att_rows if att_rows else [["N/A"]*7],
        att_col_w
    )
    story.append(att_tbl)

    # Footer note
    story.append(Spacer(1, 4*mm))
    story.append(HRFlowable(width="100%", thickness=0.5, color=lgray))
    story.append(Spacer(1, 1*mm))
    story.append(Paragraph(
        f"UNOFFICIAL — FOR REFERENCE ONLY · Generated: {now_str} · "
        f"dji_process.sh v1.0.0",
        style_footer
    ))

    # --------------- Build ---------------
    def on_page(canvas, doc):
        canvas.saveState()
        # Bottom footer on every page
        canvas.setFont("Helvetica", 6.5)
        canvas.setFillColor(mgray)
        footer_text = (f"UNOFFICIAL — FOR REFERENCE ONLY  ·  "
                       f"Generated: {now_str}  ·  Page {doc.page}")
        canvas.drawCentredString(PAGE_W / 2, 8*mm, footer_text)
        canvas.restoreState()

    doc.build(story, onFirstPage=on_page, onLaterPages=on_page)

    if verbose:
        print(f"[VERB] PDF written: {output_path}", flush=True)
    return True

# ---------------------------------------------------------------------------
# Main entry point
# ---------------------------------------------------------------------------
def main():
    args = parse_args()
    video_path  = args.video
    output_dir  = args.output_dir
    basename    = args.basename
    verbose     = args.verbose
    operator    = args.operator
    flight_num  = args.flight_num

    os.makedirs(output_dir, exist_ok=True)

    # Paths
    gpx_path  = os.path.join(output_dir, f"{basename}.gpx")
    alt_path  = os.path.join(output_dir, f"{basename}_altitude.png")
    map_path  = os.path.join(output_dir, f"{basename}_gpsmap.png")
    pdf_path  = os.path.join(output_dir, f"{basename}_briefing.pdf")
    md_path   = os.path.join(output_dir, f"{basename}_report.md")

    results = {"gpx": False, "charts": False, "pdf": False, "md": False}

    # --- Extract telemetry ---
    print(f"[INFO] Extracting telemetry from {os.path.basename(video_path)} ...",
          flush=True)
    raw_rows = extract_telemetry(video_path, verbose=verbose)
    records  = parse_telemetry(raw_rows, verbose=verbose)

    if not records:
        print("[WARN] No telemetry records found — skipping all telemetry outputs.",
              flush=True)
        # Write empty md anyway
        stats = {}
        if not args.skip_md:
            try:
                write_markdown([], stats, md_path, basename, operator,
                               flight_num, video_path, verbose=verbose)
                results["md"] = True
            except Exception as e:
                print(f"[WARN] Markdown failed: {e}", flush=True)
        print(json.dumps(results), flush=True)
        return

    stats = compute_stats(records)
    if verbose:
        print(f"[VERB] Stats: {json.dumps({k:v for k,v in stats.items() if not isinstance(v, float) or True}, default=str)}", flush=True)

    # --- GPX ---
    if not args.skip_gpx:
        try:
            ok = write_gpx(records, gpx_path, basename, verbose=verbose)
            results["gpx"] = ok
            if ok:
                print(f"[OK]   GPX: {gpx_path}", flush=True)
        except Exception as e:
            print(f"[WARN] GPX generation failed: {e}", flush=True)
            if verbose:
                traceback.print_exc()

    # --- Charts ---
    if not args.skip_charts:
        try:
            ok_alt = make_altitude_chart(records, alt_path, basename, verbose=verbose)
            ok_map = make_gps_map(records, map_path, basename, verbose=verbose)
            results["charts"] = ok_alt or ok_map
            if ok_alt: print(f"[OK]   Altitude chart: {alt_path}", flush=True)
            if ok_map:  print(f"[OK]   GPS map: {map_path}", flush=True)
        except Exception as e:
            print(f"[WARN] Chart generation failed: {e}", flush=True)
            if verbose:
                traceback.print_exc()

    # --- PDF ---
    if not args.skip_pdf:
        try:
            ok = make_pdf(records, stats, pdf_path, basename, operator,
                          flight_num, video_path,
                          map_img=map_path, alt_img=alt_path,
                          verbose=verbose)
            results["pdf"] = ok
            if ok: print(f"[OK]   PDF briefing: {pdf_path}", flush=True)
        except Exception as e:
            print(f"[WARN] PDF generation failed: {e}", flush=True)
            if verbose:
                traceback.print_exc()

    # --- Markdown ---
    if not args.skip_md:
        try:
            ok = write_markdown(records, stats, md_path, basename, operator,
                                flight_num, video_path, verbose=verbose)
            results["md"] = ok
            if ok: print(f"[OK]   Markdown report: {md_path}", flush=True)
        except Exception as e:
            print(f"[WARN] Markdown generation failed: {e}", flush=True)
            if verbose:
                traceback.print_exc()

    print(json.dumps(results), flush=True)

if __name__ == "__main__":
    main()
PYEOF
}

# -----------------------------------------------------------------------------
# FFmpeg transcode
# -----------------------------------------------------------------------------
transcode_video() {
    local input="$1"
    local output="$2"
    local bitrate_num

    bitrate_num="${BITRATE//[^0-9]/}"

    local codec_lib
    if [[ "${CODEC}" == "h265" ]]; then
        codec_lib="libx265"
    else
        codec_lib="libx264"
    fi

    local maxrate bufsize
    maxrate="$(( bitrate_num * 4 / 3 ))M"
    bufsize="$(( bitrate_num * 2 ))M"

    local extra_args=()
    if [[ -n "${CRF}" ]]; then
        extra_args+=("-crf" "${CRF}")
    fi

    log_info "Transcoding: $(basename "${input}") → $(basename "${output}")"
    log_verbose "  Codec: ${codec_lib}, Bitrate: ${BITRATE}, Preset: ${PRESET}"

    if [[ "${DRY_RUN}" == "true" ]]; then
        log_info "[DRY-RUN] ffmpeg -i \"${input}\" -map 0:v:0 -c:v ${codec_lib} -b:v ${BITRATE} ..."
        return 0
    fi

    local ffmpeg_args=(
        -i "${input}"
        -map 0:v:0
        -c:v "${codec_lib}"
        -b:v "${BITRATE}"
        -maxrate "${maxrate}"
        -bufsize "${bufsize}"
        -preset "${PRESET}"
        -profile:v high
        -level:v 5.1
        -pix_fmt yuv420p
        -color_range tv
        -colorspace bt709
        -color_trc bt709
        -color_primaries bt709
        -movflags +faststart
        "${extra_args[@]}"
        "${output}"
    )

    local ffmpeg_log
    if [[ "${VERBOSE}" == "true" ]]; then
        ffmpeg "${ffmpeg_args[@]}"
    else
        ffmpeg -y -loglevel error "${ffmpeg_args[@]}"
    fi
}

# -----------------------------------------------------------------------------
# Process a single video file
# -----------------------------------------------------------------------------
SUMMARY_KEYS=()  # parallel arrays (bash 3.2 compatible)
SUMMARY_VALS=()

process_video() {
    local video_path="$1"
    local flight_num="$2"
    local start_ts
    start_ts=$(date +%s)

    local basename
    basename="$(basename "${video_path}" .mp4)"
    basename="${basename%%.MP4}"

    # All outputs go directly into OUTPUT_DIR (flat structure, no per-video subdirs)
    local pkg_dir="${OUTPUT_DIR}"

    log_bold ""
    log_bold "════════════════════════════════════════════════"
    log_bold " Processing: ${basename}"
    log_bold "════════════════════════════════════════════════"
    log_info "Input:  ${video_path}"
    log_info "Output: ${pkg_dir}"

    if [[ "${DRY_RUN}" != "true" ]]; then
        mkdir -p "${pkg_dir}"
    fi

    local status_parts=()
    local video_out="${pkg_dir}/${basename}_transcoded.mp4"

    # ---- Transcode ----
    if [[ "${SKIP_VIDEO}" == "false" ]]; then
        if [[ -f "${video_out}" && "${FORCE}" == "false" ]]; then
            log_warn "Transcoded file exists, skipping (use -f to force): ${video_out}"
            status_parts+=("video:SKIP")
        else
            if transcode_video "${video_path}" "${video_out}"; then
                log_ok "Transcoded: ${video_out}"
                status_parts+=("video:OK")
            else
                log_error "Transcode FAILED for ${basename}"
                status_parts+=("video:FAIL")
            fi
        fi
    else
        status_parts+=("video:SKIP")
    fi

    # ---- Python telemetry + charts + PDF ----
    if [[ "${SKIP_TELEMETRY}" == "false" ]]; then
        local py_args=(
            --video    "${video_path}"
            --output-dir "${pkg_dir}"
            --basename "${basename}"
            --operator "${OPERATOR}"
            --flight-num "${flight_num}"
        )
        [[ "${SKIP_CHARTS}" == "true" ]] && py_args+=(--skip-charts)
        [[ "${SKIP_PDF}"    == "true" ]] && py_args+=(--skip-pdf)
        [[ "${VERBOSE}"     == "true" ]] && py_args+=(--verbose)

        log_info "Running telemetry processor..."
        if [[ "${DRY_RUN}" == "true" ]]; then
            log_info "[DRY-RUN] python3 ${PYTHON_SCRIPT} ${py_args[*]}"
            status_parts+=("telemetry:DRY")
        else
            local py_output
            py_output=$(python3 "${PYTHON_SCRIPT}" "${py_args[@]}" 2>&1) || true

            # Print python output line by line for progress visibility
            while IFS= read -r line; do
                if [[ "${line}" == \[OK\]* ]]; then
                    log_ok "  ${line#\[OK\]   }"
                elif [[ "${line}" == \[WARN\]* ]]; then
                    log_warn "  ${line#\[WARN\] }"
                elif [[ "${line}" == \[ERROR\]* ]]; then
                    log_error "  ${line#\[ERROR\] }"
                elif [[ "${line}" == \[INFO\]* ]]; then
                    log_info "  ${line#\[INFO\] }"
                elif [[ "${line}" == \[VERB\]* ]]; then
                    log_verbose "  ${line#\[VERB\]  }"
                elif [[ "${line}" == \{* ]]; then
                    # JSON result line — parse it
                    local gpx_ok charts_ok pdf_ok md_ok
                    gpx_ok=$(echo "${line}"    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('gpx',False))"    2>/dev/null || echo "False")
                    charts_ok=$(echo "${line}" | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('charts',False))" 2>/dev/null || echo "False")
                    pdf_ok=$(echo "${line}"    | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('pdf',False))"    2>/dev/null || echo "False")
                    md_ok=$(echo "${line}"     | python3 -c "import sys,json; d=json.load(sys.stdin); print(d.get('md',False))"     2>/dev/null || echo "False")
                    status_parts+=("gpx:${gpx_ok}" "charts:${charts_ok}" "pdf:${pdf_ok}" "md:${md_ok}")
                else
                    [[ -n "${line}" ]] && echo "          ${line}"
                fi
            done <<< "${py_output}"
        fi
    else
        status_parts+=("telemetry:SKIP")
    fi

    # ---- Timing ----
    local end_ts
    end_ts=$(date +%s)
    local elapsed=$(( end_ts - start_ts ))

    # ---- Output size ----
    local out_size="N/A"
    if [[ -f "${video_out}" ]]; then
        out_size=$(du -sh "${video_out}" 2>/dev/null | cut -f1 || echo "N/A")
    fi

    # ---- Summary row ----
    local status_str
    status_str=$(IFS=", "; echo "${status_parts[*]}")
    SUMMARY_KEYS+=("${basename}")
    SUMMARY_VALS+=("${status_str} | size=${out_size} | time=${elapsed}s")

    log_ok "Finished ${basename} in ${elapsed}s"
}

# -----------------------------------------------------------------------------
# Print summary table
# -----------------------------------------------------------------------------
print_summary() {
    log_bold ""
    log_bold "╔══════════════════════════════════════════════════════════════════════╗"
    log_bold "║                     PROCESSING SUMMARY                              ║"
    log_bold "╠══════════════════════════════════════════════════════════════════════╣"
    printf "${BOLD}║ %-30s %-12s %-26s ║${RESET}\n" "BASENAME" "SIZE" "STATUS"
    log_bold "╠══════════════════════════════════════════════════════════════════════╣"

    local total=0
    local ok_count=0
    local idx
    for idx in "${!SUMMARY_KEYS[@]}"; do
        local key="${SUMMARY_KEYS[$idx]}"
        local row="${SUMMARY_VALS[$idx]}"
        local size
        size=$(echo "${row}" | grep -oE 'size=[^ |]+' | cut -d= -f2 || echo "N/A")
        local time_taken
        time_taken=$(echo "${row}" | grep -oE 'time=[^ |]+' | cut -d= -f2 || echo "N/A")
        local status_part
        status_part=$(echo "${row}" | cut -d'|' -f1 | xargs)
        printf "║ %-30s %-12s %-26s ║\n" \
            "${key:0:30}" "${size}" "${time_taken} | ${status_part:0:20}"
        (( total++ )) || true
        [[ "${row}" != *"FAIL"* ]] && (( ok_count++ )) || true
    done

    log_bold "╠══════════════════════════════════════════════════════════════════════╣"
    printf "${BOLD}║ Total: %-3s videos processed, %-3s succeeded%-21s ║${RESET}\n" \
        "${total}" "${ok_count}" ""
    log_bold "╚══════════════════════════════════════════════════════════════════════╝"
}

# -----------------------------------------------------------------------------
# Collect video files
# -----------------------------------------------------------------------------
collect_videos() {
    local input_dir="$1"
    local videos=()

    if [[ "${RECURSIVE}" == "true" ]]; then
        while IFS= read -r -d $'\0' f; do
            videos+=("$f")
        done < <(find "${input_dir}" \( -iname "*.mp4" \) -print0 | sort -z)
    else
        for ext in mp4 MP4; do
            for f in "${input_dir}"/*.${ext}; do
                [[ -f "$f" ]] && videos+=("$f")
            done
        done
        # Sort and deduplicate
        IFS=$'\n' videos=($(printf "%s\n" "${videos[@]}" | sort -u))
        unset IFS
    fi

    printf "%s\n" "${videos[@]}"
}

# -----------------------------------------------------------------------------
# Main
# -----------------------------------------------------------------------------
main() {
    parse_args "$@"
    validate_args
    check_dependencies
    write_python_script

    log_bold ""
    log_bold "  DJI Post-Processing Pipeline v${VERSION}"
    log_bold "  Input:    ${INPUT_DIR}"
    log_bold "  Output:   ${OUTPUT_DIR}"
    log_bold "  Codec:    ${CODEC}, Bitrate: ${BITRATE}, Preset: ${PRESET}"
    log_bold "  Operator: ${OPERATOR}"
    [[ "${DRY_RUN}"   == "true" ]] && log_warn "  DRY-RUN mode — no files will be written"
    [[ "${RECURSIVE}" == "true" ]] && log_info "  Recursive mode enabled"
    log_bold ""

    if [[ "${DRY_RUN}" != "true" ]]; then
        mkdir -p "${OUTPUT_DIR}"
    fi

    # bash 3.2 compatible array population (no mapfile)
    VIDEOS=()
    while IFS= read -r _v; do
        [[ -n "$_v" ]] && VIDEOS+=("$_v")
    done < <(collect_videos "${INPUT_DIR}")

    if [[ ${#VIDEOS[@]} -eq 0 ]]; then
        log_warn "No .mp4 / .MP4 files found in: ${INPUT_DIR}"
        exit 0
    fi

    log_info "Found ${#VIDEOS[@]} video file(s)."

    local flight_num=1
    for video in "${VIDEOS[@]}"; do
        process_video "${video}" "${flight_num}"
        (( flight_num++ )) || true
    done

    # Cleanup temp Python script
    rm -f "${PYTHON_SCRIPT}"

    print_summary
    log_ok "All done."
}

main "$@"
