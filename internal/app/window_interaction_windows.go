//go:build windows

package app

import (
	"log/slog"

	native "deskgrid/internal/windows"
	"github.com/wailsapp/wails/v3/pkg/application"
)

// Observe user move/resize sessions and enforce fixed-window protection.
// Unlocked windows keep Windows' proposed position without any adjustment.
func (h *Desktop) windowInteractionMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) (uintptr, bool) {
	switch message {
	case 0x216, 0x231, 0x232, 0x112, 0xa1:
	default:
		return 0, false
	}
	h.mu.Lock()
	id := ""
	for candidate, win := range h.main {
		if h.nativeReady[candidate] && uintptr(win.NativeWindow()) == hwnd {
			id = candidate
			break
		}
	}
	if id == "" {
		h.mu.Unlock()
		return 0, false
	}
	locked := h.applied[id].positionLocked
	win := h.main[id]
	allowed := !h.quitting && !h.startHiddenLocked(id) && (!h.hidden || h.peekActive)
	h.mu.Unlock()
	if locked && (message == 0x112 && (wparam&0xfff0 == 0xf010 || wparam&0xfff0 == 0xf000) || message == 0xa1 && (wparam == 2 || wparam >= 10 && wparam <= 17)) {
		return 0, true // SC_MOVE / SC_SIZE / caption and resize-border dragging.
	}
	switch message {
	case 0x231: // WM_ENTERSIZEMOVE
		if allowed && !locked {
			h.beginGeometryEdit(id, win)
		}
	case 0x232: // WM_EXITSIZEMOVE
		h.endGeometryEdit(id, win)
	case 0x216: // WM_MOVING only needs interception for protected windows.
		if (locked || !allowed) && native.KeepWindowPosition(hwnd, lparam) {
			return 1, true
		}
	}
	return 0, false
}

func (h *Desktop) startHiddenLocked(id string) bool {
	return h.hideStartWindow && id == h.startID
}

// RefreshStartVisibility runs only after the global configuration was saved.
// The retained WebView remains in main/session even while it has no click target.
func (h *Desktop) RefreshStartVisibility() {
	h.peekMu.Lock()
	g, _, _, _ := h.Service.Store.Snapshot()
	h.mu.Lock()
	h.hideStartWindow = g.HideStartWindow
	id := h.startID
	win := h.main[id]
	ready := h.nativeReady[id]
	entry, peeking := h.peekWindows[id]
	lastPeek := peeking && len(h.peekWindows) == 1
	if g.HideStartWindow {
		delete(h.showing, id)
	}
	h.mu.Unlock()
	if win == nil || !ready {
		h.peekMu.Unlock()
		return
	}
	if g.HideStartWindow {
		if lastPeek {
			h.hidePeekLocked()
			peeking = false
		}
		application.InvokeSync(func() {
			hwnd := uintptr(win.NativeWindow())
			win.Hide() // Also removes WebView composition hit-testing while retained.
			native.HideNativeWindow(hwnd)
			if peeking && native.IsNativeWindow(hwnd) {
				if err := native.RestoreDesktopWindow(hwnd, entry.placement); err != nil {
					slog.Warn("隐藏起始窗口时恢复桌面", "error", err)
					return
				}
				h.mu.Lock()
				delete(h.peekWindows, id)
				h.desktopParents[id] = native.DesktopParent(hwnd)
				h.desktopReady[id] = native.IsDesktopAttached(hwnd)
				h.mu.Unlock()
			}
		})
		h.peekMu.Unlock()
		return
	}
	if peeking {
		h.hidePeekLocked()
	}
	h.peekMu.Unlock()
	h.mount(id)
}
