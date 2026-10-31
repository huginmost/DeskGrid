package appearance

import (
	"fmt"
	"math"
	"regexp"
	"deskgrid/internal/model"
)

var color = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func ValidatePatch(p model.AppearancePatch) error {
	for _, v := range []struct {
		p      *float64
		lo, hi float64
	}{{p.Opacity, .1, 1}, {p.Scale, .75, 2}, {p.CornerRadius, 0, 24}} {
		if v.p != nil && (math.IsNaN(*v.p) || math.IsInf(*v.p, 0) || *v.p < v.lo || *v.p > v.hi) {
			return fmt.Errorf("appearance value out of range")
		}
	}
	for _, v := range []*string{p.TitleColor, p.TitleTextColor, p.BackgroundColor, p.FolderColor, p.TextColor} {
		if v != nil && !color.MatchString(*v) {
			return fmt.Errorf("color must be #RRGGBB")
		}
	}
	return nil
}
func apply(a *model.Appearance, p model.AppearancePatch) {
	if p.Opacity != nil && !math.IsNaN(*p.Opacity) && !math.IsInf(*p.Opacity, 0) {
		a.Opacity = math.Max(.1, math.Min(1, *p.Opacity))
	}
	if p.Scale != nil && !math.IsNaN(*p.Scale) && !math.IsInf(*p.Scale, 0) {
		a.Scale = math.Max(.75, math.Min(2, *p.Scale))
	}
	if p.CornerRadius != nil && !math.IsNaN(*p.CornerRadius) && !math.IsInf(*p.CornerRadius, 0) {
		a.CornerRadius = math.Max(0, math.Min(24, *p.CornerRadius))
	}
	for _, v := range []struct {
		dst *string
		src *string
	}{{&a.TitleColor, p.TitleColor}, {&a.BackgroundColor, p.BackgroundColor}, {&a.FolderColor, p.FolderColor}, {&a.TextColor, p.TextColor}} {
		if v.src != nil && color.MatchString(*v.src) {
			*v.dst = *v.src
		}
	}
}
func Resolve(g model.Global, w model.Window, systemDark bool) model.Appearance {
	a := model.Appearance{Opacity: .95, Scale: 1, CornerRadius: 7, TitleColor: "#222222", BackgroundColor: "#F4F4F4", FolderColor: "#D49A28", TextColor: "#222222"}
	if g.Theme == "dark" || (g.Theme == "system" && systemDark) {
		a.TitleColor = "#EEEEEE"
		a.BackgroundColor = "#202020"
		a.FolderColor = "#E4B95B"
		a.TextColor = "#EEEEEE"
	}
	apply(&a, g.Appearance)
	apply(&a, w.AppearanceOverrides)
	// Existing JSON without titleTextColor keeps its previous title appearance.
	a.TitleTextColor = a.TitleColor
	for _, value := range []*string{g.Appearance.TitleTextColor, w.AppearanceOverrides.TitleTextColor} {
		if value != nil && color.MatchString(*value) {
			a.TitleTextColor = *value
		}
	}
	return a
}
