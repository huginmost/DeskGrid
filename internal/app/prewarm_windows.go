//go:build windows

package app

import (
	"encoding/json"
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
	"deskgrid/internal/appearance"
	"deskgrid/internal/model"
	"deskgrid/internal/shell"
)

// One ready WebView is reused without navigating or parsing Vue again. It has
// no saved definition, is never shown, and is replenished after assignment.
func (h *Desktop) prepareMainWindow() {
	h.mu.Lock()
	if h.quitting || h.preparing || h.prepared != nil {
		h.mu.Unlock()
		return
	}
	h.preparing = true
	h.preparedSequence++
	name := fmt.Sprintf("prepared-main-%d", h.preparedSequence)
	h.mu.Unlock()
	win := h.App.Window.NewWithOptions(application.WebviewWindowOptions{Name: name, Title: "DeskGrid", URL: "/?prepared=" + name, Width: 280, Height: 300, MinWidth: 220, MinHeight: 32, Hidden: true, Frameless: true, EnableFileDrop: true, BackgroundColour: application.NewRGB(32, 32, 32), DefaultContextMenuDisabled: true, Windows: application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, WindowDidMoveDebounceMS: 300}})
	h.mu.Lock()
	h.preparing = false
	if h.quitting {
		h.mu.Unlock()
		win.Close()
		return
	}
	h.prepared = win
	h.preparedID = name
	h.preparedReady = false
	h.mu.Unlock()
	win.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		h.mu.Lock()
		if h.prepared == win {
			h.prepared = nil
			h.preparedID = ""
			h.preparedReady = false
		}
		h.mu.Unlock()
	})
}

func (h *Desktop) assignPrepared(win *application.WebviewWindow, w model.Window) {
	g, session, windows, warnings := h.Service.Store.Snapshot()
	opened := append([]string{}, session.OpenWindowIDs...)
	found := false
	for _, id := range opened {
		if id == w.ID {
			found = true
			break
		}
	}
	if !found {
		opened = append(opened, w.ID)
	}
	dark := shell.SystemDark()
	state := map[string]any{"window": w, "windows": windows, "openWindowIds": opened, "global": g, "appearance": appearance.Resolve(g, w, dark), "drives": shell.Drives(), "warnings": warnings, "status": h.Status(), "pendingPins": []string{}, "systemDark": dark}
	data, _ := json.Marshal(map[string]any{"windowId": w.ID, "snapshot": state})
	win.ExecJS("(function(data){if(new URLSearchParams(location.search).has('prepared'))window.__deskgridPendingAssignment=data;window.dispatchEvent(new CustomEvent('deskgrid:assign',{detail:data}));})(" + string(data) + ");")
}
