package goradio

import (
	"fmt"
	"regexp"
	"strings"
)

// Gradio's default theme is preserved unless colors are configured
// explicitly in Go or Python. Color values must be six-digit CSS hex.
var themeHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func validateTheme(theme *Theme) error {
	if theme == nil {
		return nil
	}
	for _, field := range []struct{ name, value string }{
		{"primary_color", theme.PrimaryColor},
		{"secondary_color", theme.SecondaryColor},
	} {
		if field.value != "" && !themeHexColor.MatchString(field.value) {
			return fmt.Errorf("theme.%s debe ser un color hexadecimal CSS #RRGGBB", field.name)
		}
	}
	return nil
}
func colorRGB(hex string) [3]int {
	var r, g, b int
	fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b)
	return [3]int{r, g, b}
}
func blendThemeColor(base string, target [3]int, weight float64) string {
	from := colorRGB(base)
	r := int(float64(from[0])*(1-weight) + float64(target[0])*weight + 0.5)
	g := int(float64(from[1])*(1-weight) + float64(target[1])*weight + 0.5)
	b := int(float64(from[2])*(1-weight) + float64(target[2])*weight + 0.5)
	return fmt.Sprintf("#%02x%02x%02x", r, g, b)
}
func themeOverrides(theme *Theme) string {
	if theme == nil {
		return ""
	}
	shades := []struct {
		n      int
		target [3]int
		mix    float64
	}{
		{50, [3]int{255, 255, 255}, .94}, {100, [3]int{255, 255, 255}, .87},
		{200, [3]int{255, 255, 255}, .72}, {300, [3]int{255, 255, 255}, .50},
		{400, [3]int{255, 255, 255}, .22}, {500, [3]int{255, 255, 255}, 0},
		{600, [3]int{0, 0, 0}, .13}, {700, [3]int{0, 0, 0}, .28},
		{800, [3]int{0, 0, 0}, .43}, {900, [3]int{0, 0, 0}, .58},
		{950, [3]int{0, 0, 0}, .70},
	}
	var css strings.Builder
	css.WriteString("\n/* Color customization set by the app author, not by visitors. */\n:root {\n")
	for _, kind := range []struct{ prefix, color string }{
		{"primary", theme.PrimaryColor}, {"secondary", theme.SecondaryColor},
	} {
		if kind.color == "" {
			continue
		}
		for _, shade := range shades {
			fmt.Fprintf(&css, "  --%s-%d: %s;\n", kind.prefix, shade.n, blendThemeColor(kind.color, shade.target, shade.mix))
		}
	}
	css.WriteString("}\n")
	return css.String()
}
