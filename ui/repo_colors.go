package ui

import (
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

const repoColorAuto = "auto"

var repoColorPalette = map[string]string{
	"coral":   "#FF8F70",
	"amber":   "#F6C667",
	"lime":    "#B5E26A",
	"emerald": "#5FD28B",
	"cyan":    "#55D8E8",
	"sky":     "#7BC7FF",
	"indigo":  "#9AA8FF",
	"pink":    "#FF8FCA",
}

const repoColorInputPlaceholder = "auto, #7CC8FF, rgb(124,199,255), 117"

type repoColorState struct {
	Raw        string
	Normalized string
	Valid      bool
}

func generatedRepoColorValue(repoName string) string {
	hash := repoColorHash(repoName)
	hue := float64(hash % 360)
	saturation := 0.58 + float64((hash>>8)%15)/100
	value := 0.82 + float64((hash>>16)%10)/100
	r, g, b := hsvToRGB(hue, saturation, value)
	return fmt.Sprintf("#%02X%02X%02X", r, g, b)
}

func repoColorHash(repoName string) uint32 {
	h := fnv.New32a()
	_, _ = h.Write([]byte(strings.ToLower(strings.TrimSpace(repoName))))
	return h.Sum32()
}

func normalizeRepoColorValue(repoName, configured string) (string, bool) {
	normalized := strings.TrimSpace(configured)
	if normalized == "" || strings.EqualFold(normalized, repoColorAuto) {
		return generatedRepoColorValue(repoName), true
	}

	if preset, ok := repoColorPalette[strings.ToLower(normalized)]; ok {
		return preset, true
	}
	if hex, ok := normalizeHexColor(normalized); ok {
		return hex, true
	}
	if ansi, ok := normalizeANSIColor(normalized); ok {
		return ansi, true
	}
	if rgb, ok := normalizeRGBColor(normalized); ok {
		return rgb, true
	}
	return "", false
}

func buildRepoColorState(repoName, configured string) repoColorState {
	raw := strings.TrimSpace(configured)
	normalized, ok := normalizeRepoColorValue(repoName, configured)
	return repoColorState{
		Raw:        raw,
		Normalized: normalized,
		Valid:      ok,
	}
}

func repoColorSaveValue(repoName, configured string) string {
	state := buildRepoColorState(repoName, configured)
	if state.Valid {
		return state.Normalized
	}
	return generatedRepoColorValue(repoName)
}

func resolveRepoAccentColor(repoName, configured string) lipgloss.Color {
	state := buildRepoColorState(repoName, configured)
	if state.Valid {
		return lipgloss.Color(state.Normalized)
	}
	return lipgloss.Color(generatedRepoColorValue(repoName))
}

func normalizeHexColor(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	if !strings.HasPrefix(trimmed, "#") {
		return "", false
	}
	digits := trimmed[1:]
	switch len(digits) {
	case 3:
		for _, ch := range digits {
			if !isHexDigit(ch) {
				return "", false
			}
		}
		return fmt.Sprintf("#%c%c%c%c%c%c",
			toUpperHex(digits[0]), toUpperHex(digits[0]),
			toUpperHex(digits[1]), toUpperHex(digits[1]),
			toUpperHex(digits[2]), toUpperHex(digits[2]),
		), true
	case 6:
		for _, ch := range digits {
			if !isHexDigit(ch) {
				return "", false
			}
		}
		return "#" + strings.ToUpper(digits), true
	default:
		return "", false
	}
}

func normalizeANSIColor(value string) (string, bool) {
	trimmed := strings.TrimSpace(value)
	v, err := strconv.Atoi(trimmed)
	if err != nil || v < 0 || v > 255 {
		return "", false
	}
	return strconv.Itoa(v), true
}

func normalizeRGBColor(value string) (string, bool) {
	trimmed := strings.TrimSpace(strings.ToLower(value))
	if strings.HasPrefix(trimmed, "rgb(") && strings.HasSuffix(trimmed, ")") {
		trimmed = strings.TrimSpace(trimmed[4 : len(trimmed)-1])
	}

	parts := strings.Split(trimmed, ",")
	if len(parts) != 3 {
		return "", false
	}

	rgb := make([]int, 3)
	for i, part := range parts {
		v, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || v < 0 || v > 255 {
			return "", false
		}
		rgb[i] = v
	}
	return fmt.Sprintf("#%02X%02X%02X", rgb[0], rgb[1], rgb[2]), true
}

func isHexDigit(ch rune) bool {
	return (ch >= '0' && ch <= '9') || (ch >= 'a' && ch <= 'f') || (ch >= 'A' && ch <= 'F')
}

func toUpperHex(ch byte) byte {
	if ch >= 'a' && ch <= 'f' {
		return ch - ('a' - 'A')
	}
	return ch
}

func hsvToRGB(h, s, v float64) (uint8, uint8, uint8) {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60.0, 2)-1))
	m := v - c

	var r1, g1, b1 float64
	switch {
	case h < 60:
		r1, g1, b1 = c, x, 0
	case h < 120:
		r1, g1, b1 = x, c, 0
	case h < 180:
		r1, g1, b1 = 0, c, x
	case h < 240:
		r1, g1, b1 = 0, x, c
	case h < 300:
		r1, g1, b1 = x, 0, c
	default:
		r1, g1, b1 = c, 0, x
	}

	return uint8(math.Round((r1 + m) * 255)),
		uint8(math.Round((g1 + m) * 255)),
		uint8(math.Round((b1 + m) * 255))
}
