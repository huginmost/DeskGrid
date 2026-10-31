package appearance

import (
	"math"
	"deskgrid/internal/model"
	"testing"
)

func TestFieldInheritance(t *testing.T) {
	g := model.DefaultGlobal()
	v := "#123456"
	w := model.DefaultWindow("collection", "x", "")
	w.AppearanceOverrides.TitleColor = &v
	a := Resolve(g, w, true)
	if a.TitleColor != v || a.Scale != 1 || a.Opacity != .95 {
		t.Fatal(a)
	}
	bad := 0.0
	if ValidatePatch(model.AppearancePatch{Opacity: &bad}) == nil {
		t.Fatal("invalid opacity accepted")
	}
}
func TestResolveClampsBoundsAndIgnoresNonFinite(t *testing.T) {
	g := model.DefaultGlobal()
	bad := math.NaN()
	large := 99.0
	g.Appearance = model.AppearancePatch{Opacity: &bad, Scale: &large}
	a := Resolve(g, model.DefaultWindow("collection", "x", ""), false)
	if a.Opacity != .95 || a.Scale != 2 {
		t.Fatal(a)
	}
}
