package ui

import (
	"fmt"
	"math"
	"strconv"
	"sync"

	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

type rgb256 struct{ r, g, b float64 }

func parseThemeRGB(s string) rgb256 {
	v, _ := strconv.ParseUint(s[1:], 16, 24)
	return rgb256{float64(v >> 16), float64((v >> 8) & 255), float64(v & 255)}
}
func paletteRGB(i int) rgb256 {
	if i >= 232 {
		v := float64(8 + 10*(i-232))
		return rgb256{v, v, v}
	}
	levels := [6]float64{0, 95, 135, 175, 215, 255}
	i -= 16
	return rgb256{levels[i/36], levels[(i/6)%6], levels[i%6]}
}
func luminance(c rgb256) float64 {
	f := func(v float64) float64 {
		v /= 255
		if v <= 0.04045 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return .2126*f(c.r) + .7152*f(c.g) + .0722*f(c.b)
}
func contrast256(a, b rgb256) float64 {
	x, y := luminance(a), luminance(b)
	if x < y {
		x, y = y, x
	}
	return (x + .05) / (y + .05)
}
func nearest256(hex string, against string, minimum float64) string {
	target := parseThemeRGB(hex)
	bg := rgb256{}
	if against != "" {
		bg = parseThemeRGB(against)
	}
	best, distance := rgb256{}, math.Inf(1)
	for i := 16; i < 256; i++ {
		c := paletteRGB(i)
		if against != "" && contrast256(c, bg) < minimum {
			continue
		}
		d := math.Pow(c.r-target.r, 2) + math.Pow(c.g-target.g, 2) + math.Pow(c.b-target.b, 2)
		if d < distance {
			best, distance = c, d
		}
	}
	return fmt.Sprintf("#%02x%02x%02x", int(best.r), int(best.g), int(best.b))
}

var paletteCache sync.Map // map[themeColors]themeColors

func colors256(c themeColors) themeColors {
	if cached, ok := paletteCache.Load(c); ok {
		return cached.(themeColors)
	}
	original := c
	c.background = nearest256(c.background, "", 0)
	c.primary = nearest256(c.primary, c.background, 4.5)
	c.secondary = nearest256(c.secondary, c.background, 3)
	c.accent = nearest256(c.accent, c.background, 4.5)
	c.error = nearest256(c.error, c.background, 4.5)
	c.divider = nearest256(c.divider, c.background, 3)
	c.selectedBackground = nearest256(c.selectedBackground, "", 0)
	c.selectedForeground = nearest256(c.selectedForeground, c.selectedBackground, 4.5)
	c.inactiveBackground = nearest256(c.inactiveBackground, "", 0)
	// Inactive controls use primary text, so preserve its readability too.
	if contrast256(parseThemeRGB(c.primary), parseThemeRGB(c.inactiveBackground)) < 4.5 {
		c.inactiveBackground = c.background
	}
	paletteCache.Store(original, c)
	return c
}

// paletteColor retains the exact palette entry chosen by colors256.
func paletteColor(hex string) lipgloss.Color {
	if lipgloss.ColorProfile() == termenv.ANSI256 {
		target := parseThemeRGB(hex)
		for i := 16; i < 256; i++ {
			if paletteRGB(i) == target {
				return lipgloss.Color(strconv.Itoa(i))
			}
		}
	}
	return lipgloss.Color(hex)
}
