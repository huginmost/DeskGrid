//go:build windows

package app

import (
	"deskgrid/internal/model"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Bounds in JSON express the user's preferred placement, not the temporary
// placement forced by a smaller display. Only native user move/resize loops
// may replace them. Capture the final bounds before delayed OS moves arrive.
type geometryEdit struct {
	before, after model.Bounds
	active, ready bool
	collapsed     bool
}

func (h *Desktop) beginGeometryEdit(id string, win *application.WebviewWindow) {
	b := win.Bounds()
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.main[id] != win {
		return
	}
	if h.geometryEdits == nil {
		h.geometryEdits = make(map[string]*geometryEdit)
	}
	edit := h.geometryEdits[id]
	if edit == nil || edit.collapsed != h.applied[id].collapsed {
		edit = &geometryEdit{before: model.Bounds{X: float64(b.X), Y: float64(b.Y), Width: float64(b.Width), Height: float64(b.Height), Unit: "dip"}, collapsed: h.applied[id].collapsed}
		h.geometryEdits[id] = edit
	}
	edit.active, edit.ready = true, false
	if timer := h.timers[id]; timer != nil {
		timer.Stop()
	}
}

func (h *Desktop) endGeometryEdit(id string, win *application.WebviewWindow) {
	h.mu.Lock()
	edit := h.geometryEdits[id]
	valid := h.main[id] == win && edit != nil && edit.active
	h.mu.Unlock()
	if !valid {
		return
	}
	b := win.Bounds()
	after := model.Bounds{X: float64(b.X), Y: float64(b.Y), Width: float64(b.Width), Height: float64(b.Height), Unit: "dip"}
	if screen, err := win.GetScreen(); err == nil && screen != nil {
		after.DisplayID = screen.ID
	}
	h.mu.Lock()
	if h.main[id] == win && h.geometryEdits[id] == edit {
		edit.after, edit.active, edit.ready = after, false, true
	}
	h.mu.Unlock()
	h.scheduleGeometry(id)
}

func geometryBounds(w model.Window, edit geometryEdit) model.Bounds {
	b := w.Bounds
	if edit.after.X != edit.before.X {
		b.X = edit.after.X
	}
	if edit.after.Y != edit.before.Y {
		b.Y = edit.after.Y
	}
	if edit.after.Width != edit.before.Width {
		b.Width = edit.after.Width
	}
	if !w.Collapsed && !edit.collapsed && edit.after.Height != edit.before.Height {
		b.Height = edit.after.Height
	}
	if b != w.Bounds && edit.after.DisplayID != "" {
		b.DisplayID = edit.after.DisplayID
	}
	return b
}

func (h *Desktop) preferredBounds(w model.Window) model.Bounds {
	h.mu.Lock()
	defer h.mu.Unlock()
	if edit := h.geometryEdits[w.ID]; edit != nil && edit.ready {
		return geometryBounds(w, *edit)
	}
	return w.Bounds
}
