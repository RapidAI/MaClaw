package desktop

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg" // DecodeConfig for JPEG shots
	"image/png"
	"regexp"
	"strconv"
	"strings"
)

// ShotRegion is a rectangle in screen pixels.
type ShotRegion struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

// Empty reports whether no region was asked for.
func (r ShotRegion) Empty() bool { return r.Width == 0 && r.Height == 0 && r.X == 0 && r.Y == 0 }

// ShotOptions shape one desktop screenshot. The zero value is the plain
// full-screen PNG every caller got before these options existed.
//
// MaxWidth and MaxHeight shrink the image to fit inside them (never enlarge),
// keeping the aspect ratio. Region crops screen pixels first (a zoom); the
// crop is then shrunk by MaxWidth/MaxHeight like a full shot. Format is png,
// jpeg, or webp; Quality (1..100) applies to jpeg and webp. Cursor draws the
// mouse pointer, which Xvfb captures never contain.
type ShotOptions struct {
	MaxWidth  int        `json:"max_width,omitempty"`
	MaxHeight int        `json:"max_height,omitempty"`
	Region    ShotRegion `json:"region,omitempty"`
	Format    string     `json:"format,omitempty"`
	Quality   int        `json:"quality,omitempty"`
	Cursor    bool       `json:"cursor,omitempty"`
	// Marks draws the numbered boxes of the last accessibility probe
	// (set-of-marks), as written by desktop_supervisor.py a11y.
	Marks bool `json:"marks,omitempty"`
	// Window is an X window id (0x1a2b or a decimal id) or a plain title.
	// The capture raises that window and crops the composited root. An
	// SDL or OpenGL window's own pixmap is empty, so import -window and
	// xwd of the id come back black; the root crop is the pixels on screen.
	Window string `json:"window,omitempty"`
}

// ShotMaxEdge bounds MaxWidth, MaxHeight, and a region side.
const ShotMaxEdge = 8192

// ShotDefaultQuality is used for jpeg and webp when Quality is 0.
const ShotDefaultQuality = 80

// Plain reports whether the options ask for nothing beyond the legacy shot.
func (o ShotOptions) Plain() bool {
	return o.MaxWidth == 0 && o.MaxHeight == 0 && o.Region.Empty() && (o.Format == "" || o.Format == "png") && !o.Cursor && !o.Marks && o.Window == ""
}

// Normalize validates the options and fills defaults. Format is lower-cased
// ("jpg" becomes "jpeg"); Quality is only kept for jpeg and webp.
func (o ShotOptions) Normalize() (ShotOptions, error) {
	o.Format = strings.ToLower(strings.TrimSpace(o.Format))
	switch o.Format {
	case "", "png":
		o.Format = "png"
		o.Quality = 0
	case "jpg", "jpeg":
		o.Format = "jpeg"
	case "webp":
	default:
		return o, fmt.Errorf("desktop screenshot format must be png, jpeg, or webp")
	}
	if o.Format != "png" {
		if o.Quality == 0 {
			o.Quality = ShotDefaultQuality
		}
		if o.Quality < 1 || o.Quality > 100 {
			return o, fmt.Errorf("desktop screenshot quality must be 1 to 100")
		}
	}
	if o.MaxWidth < 0 || o.MaxHeight < 0 || o.MaxWidth > ShotMaxEdge || o.MaxHeight > ShotMaxEdge {
		return o, fmt.Errorf("desktop screenshot max_width and max_height must be 0 to %d", ShotMaxEdge)
	}
	if (o.MaxWidth > 0 && o.MaxWidth < 16) || (o.MaxHeight > 0 && o.MaxHeight < 16) {
		return o, fmt.Errorf("desktop screenshot max_width and max_height must be at least 16")
	}
	if !o.Region.Empty() {
		r := o.Region
		if r.X < 0 || r.Y < 0 || r.Width < 1 || r.Height < 1 || r.Width > ShotMaxEdge || r.Height > ShotMaxEdge || r.X > ShotMaxEdge || r.Y > ShotMaxEdge {
			return o, fmt.Errorf("desktop screenshot region must have x,y >= 0 and a positive width and height")
		}
	}
	window, err := ShotWindow(o.Window)
	if err != nil {
		return o, err
	}
	o.Window = window
	if o.Window != "" {
		// The window's geometry is the crop. A region as well would be a
		// second rectangle the script cannot place on the window.
		o.Region = ShotRegion{}
	}
	return o, nil
}

// MIME is the content type of the encoded image.
func (o ShotOptions) MIME() string {
	switch o.Format {
	case "jpeg":
		return "image/jpeg"
	case "webp":
		return "image/webp"
	default:
		return "image/png"
	}
}

// Shot is one captured screenshot and how its pixels map onto the screen.
// Screen pixel = Region origin + image pixel / Scale.
type Shot struct {
	Data         []byte
	MIME         string
	ScreenWidth  int
	ScreenHeight int
	ImageWidth   int
	ImageHeight  int
	// Region is the screen rectangle shown in the image. A full shot is
	// 0,0,ScreenWidth,ScreenHeight.
	Region ShotRegion
	// Scale is image pixels per screen pixel (1 for an unscaled shot).
	Scale   float64
	CursorX int
	CursorY int
	// HasCursor is set when the capture read the pointer position.
	HasCursor bool
}

// ToScreen maps a point in this image onto screen pixels, clamped to the screen.
func (s Shot) ToScreen(x, y float64) (int, int) {
	scale := s.Scale
	if scale <= 0 {
		scale = 1
	}
	sx := int(float64(s.Region.X) + x/scale + 0.5)
	sy := int(float64(s.Region.Y) + y/scale + 0.5)
	if s.ScreenWidth > 0 {
		sx = clampInt(sx, 0, s.ScreenWidth-1)
	}
	if s.ScreenHeight > 0 {
		sy = clampInt(sy, 0, s.ScreenHeight-1)
	}
	return sx, sy
}

// FromScreen maps a screen point into this image's pixels.
func (s Shot) FromScreen(x, y int) (int, int) {
	scale := s.Scale
	if scale <= 0 {
		scale = 1
	}
	return int(float64(x-s.Region.X)*scale + 0.5), int(float64(y-s.Region.Y)*scale + 0.5)
}

func clampInt(v, lo, hi int) int {
	if hi < lo {
		return lo
	}
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// shotMetaPrefix starts the first output line of ShotScript.
const shotMetaPrefix = "MACLAW_SHOT"

var shotWindowIDPattern = regexp.MustCompile(`^(?:0x[0-9a-fA-F]{1,8}|[0-9]{1,10})$`)

// ShotWindow accepts an X window id or a plain title. Empty is no window.
// A title may contain letters and spaces. Shell and regex metacharacters
// are rejected so the id or title can be placed in the capture script.
func ShotWindow(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", nil
	}
	if len(raw) > 80 {
		return "", fmt.Errorf("desktop screenshot window is too long")
	}
	if shotWindowIDPattern.MatchString(raw) {
		if shotWindowIDValue(raw) == 0 {
			return "", fmt.Errorf("desktop screenshot window id is empty")
		}
		return raw, nil
	}
	if strings.ContainsAny(raw, "\r\n\x00`$\\;|&<>\"'*?[]{}()^") {
		return "", fmt.Errorf("desktop screenshot window must be a window id or a plain title")
	}
	return raw, nil
}

func shotWindowIDValue(raw string) uint32 {
	base := 10
	digits := raw
	if strings.HasPrefix(strings.ToLower(raw), "0x") {
		base = 16
		digits = raw[2:]
	}
	n, err := strconv.ParseUint(digits, base, 32)
	if err != nil {
		return 0
	}
	return uint32(n)
}

func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// xdotoolLiteralName escapes a title for xdotool search --name, which is a
// regular expression. The title is already free of shell metacharacters.
func xdotoolLiteralName(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch r {
		case '\\', '.', '^', '$', '*', '+', '?', '(', ')', '[', ']', '{', '}', '|':
			b.WriteByte('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// shotWindowPrelude resolves and raises the window before the root capture.
// Raising first is what puts an SDL or OpenGL window into the composite;
// capturing the window id itself reads an empty pixmap.
func shotWindowPrelude(window string) string {
	if window == "" {
		return ""
	}
	quoted := shellSingleQuote(window)
	var b strings.Builder
	if shotWindowIDPattern.MatchString(window) {
		b.WriteString("wid=" + quoted + "\n")
	} else {
		// The largest match is the frame on screen. Visible windows win; a
		// minimized game is found by the second search and mapped below.
		pattern := shellSingleQuote(xdotoolLiteralName(window))
		b.WriteString(`wid=
best=0
consider() {
  for id in "$@"; do
    geo=$(xwininfo -id "$id" 2>/dev/null) || continue
    gw=$(printf '%s\n' "$geo" | awk '/^[[:space:]]*Width:/ {print $NF; exit}')
    gh=$(printf '%s\n' "$geo" | awk '/^[[:space:]]*Height:/ {print $NF; exit}')
    case "$gw" in ''|*[!0-9]*) continue ;; esac
    case "$gh" in ''|*[!0-9]*) continue ;; esac
    # A tooltip or input-only window can share the title. Keeping it would
    # hide a minimized game that the second search is there to find.
    if [ "$gw" -lt 32 ] || [ "$gh" -lt 32 ]; then continue; fi
    ga=$((gw*gh))
    if [ "$ga" -gt "$best" ]; then best=$ga; wid=$id; fi
  done
}
consider $(xdotool search --onlyvisible --name ` + pattern + ` 2>/dev/null)
if [ -z "$wid" ]; then
  consider $(xdotool search --name ` + pattern + ` 2>/dev/null)
fi
[ -n "$wid" ] || { echo "desktop screenshot window was not found" >&2; rm -rf "$d"; exit 1; }
`)
	}
	// A missing id fails here. Importing the root first would spend a full
	// frame grab and only then report that the window does not exist.
	b.WriteString(`xwininfo -id "$wid" >/dev/null 2>&1 || { echo "desktop screenshot window was not found" >&2; rm -rf "$d"; exit 1; }
`)
	// Map first so a minimized game is on the composite, then raise and
	// activate. --sync returns when the window manager has focused it;
	// the sleep is one frame for an SDL buffer to present.
	b.WriteString(`xdotool windowmap "$wid" >/dev/null 2>&1 || true
xdotool windowraise "$wid" >/dev/null 2>&1 || true
xdotool windowactivate --sync "$wid" >/dev/null 2>&1 || true
sleep 0.3
`)
	return b.String()
}

// shotWindowGeometry reads the window frame from xwininfo and sets the crop.
func shotWindowGeometry() string {
	return `geo=$(xwininfo -id "$wid" 2>/dev/null) || { echo "desktop screenshot window was not found" >&2; rm -rf "$d"; exit 1; }
ax=$(printf '%s\n' "$geo" | awk '/Absolute upper-left X:/ {print $NF; exit}')
ay=$(printf '%s\n' "$geo" | awk '/Absolute upper-left Y:/ {print $NF; exit}')
aw=$(printf '%s\n' "$geo" | awk '/^[[:space:]]*Width:/ {print $NF; exit}')
ah=$(printf '%s\n' "$geo" | awk '/^[[:space:]]*Height:/ {print $NF; exit}')
case "$ax" in ''|*[!0-9-]*|-*[!0-9]*|-) echo "desktop screenshot window has no geometry" >&2; rm -rf "$d"; exit 1 ;; esac
case "$ay" in ''|*[!0-9-]*|-*[!0-9]*|-) echo "desktop screenshot window has no geometry" >&2; rm -rf "$d"; exit 1 ;; esac
case "$aw" in ''|*[!0-9]*) echo "desktop screenshot window has no geometry" >&2; rm -rf "$d"; exit 1 ;; esac
case "$ah" in ''|*[!0-9]*) echo "desktop screenshot window has no geometry" >&2; rm -rf "$d"; exit 1 ;; esac
[ "$aw" -ge 1 ] && [ "$ah" -ge 1 ] || { echo "desktop screenshot window has no geometry" >&2; rm -rf "$d"; exit 1; }
[ "$ax" -lt 0 ] && aw=$((aw+ax)) && ax=0
[ "$ay" -lt 0 ] && ah=$((ah+ay)) && ay=0
[ "$aw" -ge 1 ] && [ "$ah" -ge 1 ] || { echo "desktop screenshot window is outside the ${sw}x${sh} screen" >&2; rm -rf "$d"; exit 1; }
[ "$ax" -lt "$sw" ] && [ "$ay" -lt "$sh" ] || { echo "desktop screenshot window is outside the ${sw}x${sh} screen" >&2; rm -rf "$d"; exit 1; }
[ $((ax+aw)) -le "$sw" ] || aw=$((sw-ax))
[ $((ay+ah)) -le "$sh" ] || ah=$((sh-ay))
[ "$aw" -ge 1 ] && [ "$ah" -ge 1 ] || { echo "desktop screenshot window is outside the ${sw}x${sh} screen" >&2; rm -rf "$d"; exit 1; }
rx=$ax; ry=$ay; rw=$aw; rh=$ah
`
}

// shotWindowBlankGuard rejects a crop that is still an empty pixmap. A
// uniform dark splash (mean above the cutoff) is kept. The check is skipped
// when ImageMagick cannot report the statistic, so a policy that blocks
// info: does not fail a real capture.
func shotWindowBlankGuard() string {
	return `stats=$(convert "$f" -format '%[fx:mean] %[fx:standard_deviation]' info: 2>/dev/null) || stats=
if [ -n "$stats" ] && echo "$stats" | awk '($1+0 < 0.02 && $2+0 < 0.004) { bad=1 } END { exit (bad ? 0 : 1) }'; then
  echo "desktop screenshot window is blank after raising it. Call action=screenshot with the same window again." >&2
  rm -rf "$d"
  exit 1
fi
`
}

// ShotScript is the container-side capture for options that are not Plain.
// It captures like ScreenshotScript, optionally saves the full PNG under
// name, then draws the pointer, crops, shrinks, and encodes with ImageMagick
// convert. The first line is
//
//	MACLAW_SHOT <screen w> <screen h> <crop x> <crop y> <crop w> <crop h> <image w> <image h> <cursor x|-> <cursor y|->
//
// and the second is the base64 image. Plain options return the legacy
// script, whose output DecodeShot also reads.
func ShotScript(name string, opts ShotOptions) (string, error) {
	opts, err := opts.Normalize()
	if err != nil {
		return "", err
	}
	if opts.Plain() {
		return ScreenshotScriptFor(name)
	}
	safe, err := DesktopShotFileName(name)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	b.WriteString(`d=$(mktemp -d) || exit 1
f="$d/screen.png"
o="$d/out"
command -v convert >/dev/null 2>&1 || { echo "desktop screenshot options need ImageMagick convert" >&2; rm -rf "$d"; exit 1; }
`)
	b.WriteString(shotWindowPrelude(opts.Window))
	b.WriteString(`import -window root "png:$f" >/dev/null 2>&1 || { rm -rf "$d"; exit 1; }
`)
	// A window shot is saved after the crop. Saving $f here would store the
	// whole desktop, including whatever else is open beside the window.
	if safe != "" && opts.Window == "" {
		path := desktopShotDir + "/" + safe
		b.WriteString("mkdir -p " + desktopShotDir + ` && cp "$f" ` + path + " && chmod 644 " + path + ` || { rm -rf "$d"; exit 1; }` + "\n")
	}
	b.WriteString(`set -- $(identify -format '%w %h' "$f" 2>/dev/null)
sw=$1; sh=$2
[ -n "$sw" ] && [ -n "$sh" ] || { rm -rf "$d"; exit 1; }
cx=-; cy=-
eval "$(xdotool getmouselocation --shell 2>/dev/null | grep -E '^[XY]=[0-9]+$')"
[ -n "$X" ] && cx=$X && cy=$Y
`)
	// Draw the pointer on the full screen before any crop, so a zoom shows
	// it where it is.
	args := []string{`"$f"`}
	if opts.Cursor {
		b.WriteString(`if [ "$cx" != "-" ]; then
  p="$cx,$cy $cx,$((cy+17)) $((cx+4)),$((cy+13)) $((cx+7)),$((cy+20)) $((cx+10)),$((cy+19)) $((cx+7)),$((cy+12)) $((cx+12)),$((cy+12))"
  convert "$f" -fill black -stroke white -strokewidth 1 -draw "polygon $p" "png:$d/draw.png" >/dev/null 2>&1 || { rm -rf "$d"; exit 1; }
  mv "$d/draw.png" "$f"
fi
`)
	}
	if opts.Marks {
		// -draw @file is blocked by Debian's ImageMagick policy; the
		// primitives go inline instead (they hold only numbers and colors).
		b.WriteString(`m="/tmp/.maclaw-a11y-$(echo "${DISPLAY#:}" | cut -d. -f1).mvg"
if [ -s "$m" ]; then
  convert "$f" -draw "$(cat "$m")" "png:$d/draw.png" >/dev/null 2>&1 || { rm -rf "$d"; exit 1; }
  mv "$d/draw.png" "$f"
fi
`)
	}
	b.WriteString("rx=0; ry=0; rw=$sw; rh=$sh\n")
	rx, ry, rw, rh := "$rx", "$ry", "$rw", "$rh"
	if opts.Window != "" {
		b.WriteString(shotWindowGeometry())
		// Crop into a second file. convert refuses to read and write the
		// same path, and an in-place crop comes back truncated.
		b.WriteString(`convert "$f" -crop "${rw}x${rh}+${rx}+${ry}" +repage "png:$d/win.png" >/dev/null 2>&1 || { rm -rf "$d"; exit 1; }
mv "$d/win.png" "$f"
`)
		b.WriteString(shotWindowBlankGuard())
		if safe != "" {
			path := desktopShotDir + "/" + safe
			b.WriteString("mkdir -p " + desktopShotDir + ` && cp "$f" ` + path + " && chmod 644 " + path + ` || { rm -rf "$d"; exit 1; }` + "\n")
		}
	} else if !opts.Region.Empty() {
		r := opts.Region
		b.WriteString(fmt.Sprintf(`rx=%d; ry=%d; rw=%d; rh=%d
[ "$rx" -lt "$sw" ] && [ "$ry" -lt "$sh" ] || { echo "desktop screenshot region is outside the ${sw}x${sh} screen" >&2; rm -rf "$d"; exit 1; }
[ $((rx+rw)) -le "$sw" ] || rw=$((sw-rx))
[ $((ry+rh)) -le "$sh" ] || rh=$((sh-ry))
`, r.X, r.Y, r.Width, r.Height))
		args = append(args, `-crop "${rw}x${rh}+${rx}+${ry}"`, "+repage")
	}
	if opts.MaxWidth > 0 || opts.MaxHeight > 0 {
		w, h := "", ""
		if opts.MaxWidth > 0 {
			w = strconv.Itoa(opts.MaxWidth)
		}
		if opts.MaxHeight > 0 {
			h = strconv.Itoa(opts.MaxHeight)
		}
		// ">" only shrinks. A triangle filter keeps text legible and is cheap.
		args = append(args, "-filter", "Triangle", "-resize", `"`+w+"x"+h+`>"`)
	}
	if opts.Format != "png" {
		args = append(args, "-quality", strconv.Itoa(opts.Quality))
	}
	if opts.Format == "jpeg" {
		// JPEG has no alpha; a flat background avoids a black fill.
		args = append(args, "-background", "white", "-alpha", "remove")
	}
	if opts.Format == "png" && opts.MaxWidth == 0 && opts.MaxHeight == 0 && len(args) == 1 {
		// The pixels are already the image to send. A second convert only
		// re-encodes the same PNG.
		b.WriteString("cp \"$f\" \"$o\" || { rm -rf \"$d\"; exit 1; }\n")
	} else {
		args = append(args, `"`+opts.Format+`:$o"`)
		b.WriteString("convert " + strings.Join(args, " ") + " >/dev/null 2>&1 || { rm -rf \"$d\"; exit 1; }\n")
	}
	b.WriteString(`set -- $(identify -format '%w %h' "$o" 2>/dev/null | head -n 1)
ow=$1; oh=$2
[ -n "$ow" ] && [ -n "$oh" ] || { rm -rf "$d"; exit 1; }
echo "` + shotMetaPrefix + ` $sw $sh ` + rx + " " + ry + " " + rw + " " + rh + ` $ow $oh $cx $cy"
base64 -w0 "$o"; s=$?
rm -rf "$d"
exit "$s"`)
	return b.String(), nil
}

// DecodeShot reads ShotScript or ScreenshotScript output. opts must be the
// normalized options the script was built from.
func DecodeShot(out string, opts ShotOptions) (Shot, error) {
	opts, err := opts.Normalize()
	if err != nil {
		return Shot{}, err
	}
	text := strings.TrimSpace(out)
	if !strings.HasPrefix(text, shotMetaPrefix) {
		data, err := DecodeScreenshot(text)
		if err != nil {
			return Shot{}, err
		}
		return ShotFromPNG(data)
	}
	line, rest, _ := strings.Cut(text, "\n")
	fields := strings.Fields(line)
	if len(fields) != 11 {
		return Shot{}, fmt.Errorf("screenshot metadata is malformed")
	}
	nums := make([]int, 8)
	for i := 0; i < 8; i++ {
		n, err := strconv.Atoi(fields[i+1])
		if err != nil || n < 0 || n > 4*ShotMaxEdge {
			return Shot{}, fmt.Errorf("screenshot metadata is malformed")
		}
		nums[i] = n
	}
	encoded := strings.Join(strings.Fields(rest), "")
	if encoded == "" || base64.StdEncoding.DecodedLen(len(encoded)) > MaxScreenshotBytes {
		return Shot{}, fmt.Errorf("screenshot is empty or too large")
	}
	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return Shot{}, fmt.Errorf("screenshot is not valid base64")
	}
	mime := SniffImageMIME(data)
	if mime == "" || mime != opts.MIME() {
		return Shot{}, fmt.Errorf("screenshot is not a %s image", opts.MIME())
	}
	shot := Shot{
		Data: data, MIME: mime,
		ScreenWidth: nums[0], ScreenHeight: nums[1],
		Region:     ShotRegion{X: nums[2], Y: nums[3], Width: nums[4], Height: nums[5]},
		ImageWidth: nums[6], ImageHeight: nums[7],
	}
	if shot.Region.Width <= 0 || shot.ImageWidth <= 0 || shot.ImageHeight <= 0 {
		return Shot{}, fmt.Errorf("screenshot metadata is malformed")
	}
	shot.Scale = float64(shot.ImageWidth) / float64(shot.Region.Width)
	if cx, err := strconv.Atoi(fields[9]); err == nil {
		if cy, err := strconv.Atoi(fields[10]); err == nil {
			shot.CursorX, shot.CursorY, shot.HasCursor = cx, cy, true
		}
	}
	return shot, nil
}

// ShotFromPNG describes an unscaled full-screen PNG.
func ShotFromPNG(data []byte) (Shot, error) {
	if !IsPNG(data) {
		return Shot{}, fmt.Errorf("screenshot is not a PNG image")
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		// The signature is there but the header is not readable: keep the
		// image as before, with an unknown size and an identity mapping.
		return Shot{Data: data, MIME: "image/png", Scale: 1}, nil
	}
	return Shot{
		Data: data, MIME: "image/png",
		ScreenWidth: cfg.Width, ScreenHeight: cfg.Height,
		ImageWidth: cfg.Width, ImageHeight: cfg.Height,
		Region: ShotRegion{Width: cfg.Width, Height: cfg.Height},
		Scale:  1,
	}, nil
}

// SniffImageMIME recognizes the three screenshot formats.
func SniffImageMIME(data []byte) string {
	switch {
	case IsPNG(data):
		return "image/png"
	case len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		return "image/jpeg"
	case len(data) > 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		return "image/webp"
	default:
		return ""
	}
}

// ImageSize reads width and height of a PNG or JPEG. WebP is not decoded
// here; callers use the shot metadata for it.
func ImageSize(data []byte) (int, int, bool) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return 0, 0, false
	}
	return cfg.Width, cfg.Height, true
}

// ShotHeaders are the response headers desktopd sets on a screenshot.
const (
	HeaderShotScreen = "X-Desktop-Screen-Size" // "1280x800"
	HeaderShotImage  = "X-Desktop-Image-Size"  // "1024x640"
	HeaderShotRegion = "X-Desktop-Shot-Region" // "x,y,w,h" in screen pixels
	HeaderShotScale  = "X-Desktop-Shot-Scale"  // image px per screen px
	HeaderShotCursor = "X-Desktop-Cursor"      // "x,y" screen pixels, absent when unknown
)

// ShotHeaderValues renders s for the response headers above.
func ShotHeaderValues(s Shot) map[string]string {
	out := map[string]string{
		HeaderShotScreen: fmt.Sprintf("%dx%d", s.ScreenWidth, s.ScreenHeight),
		HeaderShotImage:  fmt.Sprintf("%dx%d", s.ImageWidth, s.ImageHeight),
		HeaderShotRegion: fmt.Sprintf("%d,%d,%d,%d", s.Region.X, s.Region.Y, s.Region.Width, s.Region.Height),
		HeaderShotScale:  strconv.FormatFloat(s.Scale, 'f', 6, 64),
	}
	if s.HasCursor {
		out[HeaderShotCursor] = fmt.Sprintf("%d,%d", s.CursorX, s.CursorY)
	}
	return out
}

// ParseShotHeaders fills the geometry of a shot from response headers. A
// response from an older service without them describes a full PNG.
func ParseShotHeaders(get func(string) string, data []byte) (Shot, error) {
	mime := SniffImageMIME(data)
	if mime == "" {
		return Shot{}, fmt.Errorf("screenshot is not a png, jpeg, or webp image")
	}
	screen := strings.TrimSpace(get(HeaderShotScreen))
	if screen == "" {
		return ShotFromPNG(data)
	}
	s := Shot{Data: data, MIME: mime}
	if _, err := fmt.Sscanf(screen, "%dx%d", &s.ScreenWidth, &s.ScreenHeight); err != nil {
		return Shot{}, fmt.Errorf("screenshot size header is malformed")
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(get(HeaderShotImage)), "%dx%d", &s.ImageWidth, &s.ImageHeight); err != nil {
		return Shot{}, fmt.Errorf("screenshot size header is malformed")
	}
	if _, err := fmt.Sscanf(strings.TrimSpace(get(HeaderShotRegion)), "%d,%d,%d,%d", &s.Region.X, &s.Region.Y, &s.Region.Width, &s.Region.Height); err != nil {
		return Shot{}, fmt.Errorf("screenshot region header is malformed")
	}
	if s.Region.Width <= 0 || s.ImageWidth <= 0 {
		return Shot{}, fmt.Errorf("screenshot size header is malformed")
	}
	s.Scale = float64(s.ImageWidth) / float64(s.Region.Width)
	if cursor := strings.TrimSpace(get(HeaderShotCursor)); cursor != "" {
		if _, err := fmt.Sscanf(cursor, "%d,%d", &s.CursorX, &s.CursorY); err == nil {
			s.HasCursor = true
		}
	}
	return s, nil
}
