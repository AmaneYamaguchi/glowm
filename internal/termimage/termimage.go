package termimage

import (
	"encoding/base64"
	"os"
	"strconv"
	"strings"
	"sync"
)

type Format int

const (
	FormatNone Format = iota
	FormatIterm2
	FormatKitty
	FormatSixel
)

var (
	detectOnce   sync.Once
	detectedFmt  Format
)

// Detect determines the terminal's image-protocol support. The result is
// cached so the DA1 probe inside isSixel() does not add latency on every
// subsequent call.
func Detect() Format {
	detectOnce.Do(func() {
		detectedFmt = detectUncached()
	})
	return detectedFmt
}

// resetDetectCache clears the cached Detect result. Intended for tests that
// vary terminal env vars between cases.
func resetDetectCache() {
	detectOnce = sync.Once{}
	detectedFmt = FormatNone
}

func detectUncached() Format {
	if f, ok := formatFromEnv(); ok {
		return f
	}
	if isIterm2() {
		return FormatIterm2
	}
	if supportsKittyGraphics() {
		return FormatKitty
	}
	if isSixel() {
		return FormatSixel
	}
	return FormatNone
}

func Encode(format Format, png []byte) string {
	return EncodeWithWidth(format, png, 0)
}

func EncodeWithWidth(format Format, png []byte, widthCells int) string {
	switch format {
	case FormatIterm2:
		return encodeIterm2(png, widthCells)
	case FormatKitty:
		return encodeKitty(png, widthCells)
	case FormatSixel:
		// TODO: widthCells is not honored for Sixel; go-sixel sizes output by
		// source pixels. Scaling would require pre-resizing the PNG.
		return encodeSixel(png)
	default:
		return ""
	}
}

// formatFromEnv honors GLOWM_IMAGE_PROTOCOL (kitty, iterm2, sixel, none) so
// users can force a protocol when auto-detection fails, e.g. inside a
// multiplexer that hides the outer terminal's identity.
func formatFromEnv() (Format, bool) {
	switch strings.ToLower(os.Getenv("GLOWM_IMAGE_PROTOCOL")) {
	case "kitty":
		return FormatKitty, true
	case "iterm2":
		return FormatIterm2, true
	case "sixel":
		return FormatSixel, true
	case "none":
		return FormatNone, true
	}
	return FormatNone, false
}

func isIterm2() bool {
	return os.Getenv("TERM_PROGRAM") == "iTerm.app"
}

// supportsKittyGraphics reports whether the current terminal renders images
// via the Kitty graphics protocol. This covers Kitty itself, Ghostty (native),
// and Ghostty wrapped by a multiplexer such as tmux or herdr (which rewrite
// TERM_PROGRAM, so we rely on the multiplexer marker + GHOSTTY_RESOURCES_DIR
// together to avoid false positives when the env var leaks into unrelated
// children such as SSH with SendEnv).
func supportsKittyGraphics() bool {
	if os.Getenv("KITTY_WINDOW_ID") != "" {
		return true
	}
	term := os.Getenv("TERM")
	if strings.Contains(term, "xterm-kitty") {
		return true
	}
	if os.Getenv("TERM_PROGRAM") == "ghostty" || strings.Contains(term, "xterm-ghostty") {
		return true
	}
	if inMultiplexer() && os.Getenv("GHOSTTY_RESOURCES_DIR") != "" {
		return true
	}
	return false
}

// inMultiplexer reports whether we run inside a multiplexer known to forward
// Kitty graphics to the outer terminal. herdr does so by default
// (kitty_graphics = true).
func inMultiplexer() bool {
	return os.Getenv("TMUX") != "" || os.Getenv("TERM_PROGRAM") == "herdr"
}

func encodeIterm2(png []byte, widthCells int) string {
	b64 := base64.StdEncoding.EncodeToString(png)
	meta := "inline=1;preserveAspectRatio=1"
	if widthCells > 0 {
		meta += ";width=" + strconv.Itoa(widthCells)
	}
	return "\x1b]1337;File=" + meta + ":" + b64 + "\x07"
}

func encodeKitty(png []byte, widthCells int) string {
	b64 := base64.StdEncoding.EncodeToString(png)
	const chunkSize = 4096
	var b strings.Builder
	for i := 0; i < len(b64); i += chunkSize {
		end := i + chunkSize
		if end > len(b64) {
			end = len(b64)
		}
		more := "0"
		if end < len(b64) {
			more = "1"
		}
		if i == 0 {
			// First chunk: include full control parameters.
			b.WriteString("\x1b_Gf=100,a=T,")
			if widthCells > 0 {
				b.WriteString("c=")
				b.WriteString(strconv.Itoa(widthCells))
				b.WriteString(",")
			}
			b.WriteString("m=")
			b.WriteString(more)
		} else {
			// Subsequent chunks: only continuation flag.
			b.WriteString("\x1b_Gm=")
			b.WriteString(more)
		}
		b.WriteString(";")
		b.WriteString(b64[i:end])
		b.WriteString("\x1b\\")
	}
	return b.String()
}
