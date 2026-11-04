//go:build windows

package app

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"strings"
	"time"

	"deskgrid/internal/appearance"
	"deskgrid/internal/shell"
	native "deskgrid/internal/windows"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type peekWindow struct {
	win       *application.WebviewWindow
	placement native.DesktopPlacement
	opacity   float64
}

func (h *Desktop) changePeekHotkey(to, _ string) error {
	mods, key, err := shell.ParseHotkey(to)
	if err != nil {
		return err
	}
	h.mu.Lock()
	input := h.peekInput
	h.mu.Unlock()
	if input == nil {
		var err error
		application.InvokeSync(func() { input, err = native.NewPeekInput(h.TogglePeek, h.peekMouseClick) })
		if err != nil {
			return err
		}
		h.mu.Lock()
		h.peekInput = input
		h.mu.Unlock()
	}
	application.InvokeSync(func() { input.Configure(mods, key) })
	h.mu.Lock()
	h.status["peekHotkey"] = "已禁用"
	if to != "" {
		h.status["peekHotkey"] = "已启用 " + to
	}
	h.mu.Unlock()
	if to == "" {
		go h.hidePeek()
	}
	return nil
}

func (h *Desktop) BeginPeekRecording(windowID, requestID string) error {
	if !strings.HasPrefix(windowID, "settings-") || requestID == "" {
		return fmt.Errorf("请从设置窗口录制快捷键")
	}
	h.mu.Lock()
	win := h.settings[strings.TrimPrefix(windowID, "settings-")]
	input := h.peekInput
	h.mu.Unlock()
	if win == nil || input == nil {
		return fmt.Errorf("快捷键监听尚未就绪")
	}
	var recordErr error
	application.InvokeSync(func() {
		owner := uintptr(win.NativeWindow())
		if !native.IsNativeWindow(owner) {
			recordErr = fmt.Errorf("设置窗口已关闭")
			return
		}
		input.Recording(owner, requestID, func(mods, key uint32) {
			result := map[string]any{"requestId": requestID}
			if key == 0 || key == 27 && mods == 0 {
				result["cancelled"] = true
			} else if binding, err := shell.FormatHotkey(mods, key); err != nil {
				result["error"] = err.Error()
			} else {
				result["binding"] = binding
			}
			payload, _ := json.Marshal(result)
			win.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:peek-recorded',{detail:" + string(payload) + "}));")
		})
	})
	return recordErr
}

func (h *Desktop) EndPeekRecording(windowID, requestID string) {
	h.mu.Lock()
	win := h.settings[strings.TrimPrefix(windowID, "settings-")]
	input := h.peekInput
	h.mu.Unlock()
	if win != nil && input != nil {
		application.InvokeSync(func() { input.StopRecording(uintptr(win.NativeWindow()), requestID) })
	}
}

func (h *Desktop) TogglePeek() {
	go func() {
		h.peekMu.Lock()
		defer h.peekMu.Unlock()
		h.mu.Lock()
		active := h.peekActive
		h.mu.Unlock()
		if active {
			h.hidePeekLocked()
		} else {
			h.showPeekLocked()
		}
	}()
}
func (h *Desktop) hidePeek() { h.peekMu.Lock(); defer h.peekMu.Unlock(); h.hidePeekLocked() }

func (h *Desktop) preparePeekWindow(id string) (peekWindow, bool) {
	h.mu.Lock()
	win := h.main[id]
	ready := h.nativeReady[id] && h.surfaceReady[id]
	hiddenStart := h.startHiddenLocked(id)
	_, exists := h.peekWindows[id]
	h.mu.Unlock()
	if win == nil || !ready || exists || hiddenStart {
		return peekWindow{}, false
	}
	w, err := h.Service.Store.Window(id)
	if err != nil {
		return peekWindow{}, false
	}
	g, _, _, _ := h.Service.Store.Snapshot()
	a := appearance.Resolve(g, w, shell.SystemDark())
	entry := peekWindow{win: win, opacity: a.Opacity}
	application.InvokeSync(func() {
		hwnd := uintptr(win.NativeWindow())
		if !native.IsNativeWindow(hwnd) {
			err = fmt.Errorf("窗口已关闭")
			return
		}
		if !native.IsDesktopAttached(hwnd) {
			if err = native.AttachDesktop(hwnd); err != nil {
				return
			}
		}
		entry.placement, err = native.PeekWindow(hwnd, func(active bool) {
			if active {
				h.mu.Lock()
				if h.peekActive {
					h.peekHadFocus = true
				}
				h.mu.Unlock()
				h.Interact(id)
			} else {
				win.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:focus',{detail:false}));")
				h.schedulePeekFocusCheck()
			}
		})
		if err == nil {
			// A retained start window or globally hidden window may have hidden
			// its WebView composition as well as its HWND. The host is still at
			// zero alpha, so restore that surface before its fade begins.
			win.Show()
		}
		if err != nil && native.IsDesktopAttached(hwnd) {
			_ = native.SetOpacity(hwnd, a.Opacity)
			h.mu.Lock()
			visible := !h.hidden && !h.startHiddenLocked(id) && !h.quitting
			h.mu.Unlock()
			if visible {
				native.ShowInactive(hwnd)
			}
		}
	})
	if err != nil {
		slog.Warn("临时前置窗口", "error", err)
		return peekWindow{}, false
	}
	win.ExecJS("document.documentElement.classList.remove('global-hiding');")
	h.mu.Lock()
	h.peekWindows[id] = entry
	h.desktopParents[id] = entry.placement.Parent
	h.desktopReady[id] = true
	h.mu.Unlock()
	h.clipWindow(id, win)
	return entry, true
}

func (h *Desktop) showPeekLocked() {
	h.mu.Lock()
	if h.quitting {
		h.mu.Unlock()
		return
	}
	h.peekActive = true
	h.peekTransition = true
	h.peekHadFocus = false
	h.peekWindows = map[string]peekWindow{}
	h.peekPrevious = native.ForegroundWindow()
	activeID := h.activeMain
	ids := []string{}
	for id := range h.main {
		delete(h.showing, id)
		if !h.startHiddenLocked(id) {
			ids = append(ids, id)
		}
	}
	h.mu.Unlock()
	entries := map[string]peekWindow{}
	var focus *application.WebviewWindow
	for _, id := range ids {
		if entry, ok := h.preparePeekWindow(id); ok {
			entries[id] = entry
			if focus == nil || id == activeID {
				focus = entry.win
			}
		}
	}
	if len(entries) == 0 {
		h.mu.Lock()
		h.peekActive = false
		h.peekTransition = false
		h.mu.Unlock()
		return
	}
	if focus != nil {
		application.InvokeSync(func() {
			hwnd := uintptr(focus.NativeWindow())
			acquired := native.FocusPeekWindow(hwnd)
			if err := native.RaisePeekWindow(hwnd); err != nil {
				slog.Warn("临时前置窗口", "error", err)
			}
			h.mu.Lock()
			h.peekHadFocus = h.peekHadFocus || acquired
			h.mu.Unlock()
		})
	}
	h.animatePeek(entries, true)
	h.mu.Lock()
	h.peekTransition = false
	input := h.peekInput
	h.mu.Unlock()
	if input != nil {
		input.Active(true)
	}
	h.schedulePeekFocusCheck()
}

func (h *Desktop) joinPeek(id string) {
	h.peekMu.Lock()
	defer h.peekMu.Unlock()
	h.mu.Lock()
	active := h.peekActive && !h.startHiddenLocked(id) && !h.quitting
	hiddenStart := h.startHiddenLocked(id)
	h.mu.Unlock()
	if !active {
		if !hiddenStart {
			h.mount(id)
		}
		return
	}
	h.mu.Lock()
	h.peekTransition = true
	h.mu.Unlock()
	if entry, ok := h.preparePeekWindow(id); ok {
		h.animatePeek(map[string]peekWindow{id: entry}, true)
	}
	h.mu.Lock()
	h.peekTransition = false
	h.mu.Unlock()
	h.schedulePeekFocusCheck()
}

func (h *Desktop) animatePeek(entries map[string]peekWindow, show bool) {
	g, _, _, _ := h.Service.Store.Snapshot()
	reduced := g.ReducedMotion || shell.ReducedMotion()
	duration := 180 * time.Millisecond
	if show {
		duration = 200 * time.Millisecond
	}
	start := time.Now()
	for {
		t := 1.0
		if !reduced {
			t = min(1, float64(time.Since(start))/float64(duration))
		}
		amount := 1 - math.Pow(1-t, 3)
		if show {
			amount = t * t * (3 - 2*t)
		}
		if !show {
			amount = 1 - amount
		}
		application.InvokeSync(func() {
			for id, entry := range entries {
				h.mu.Lock()
				valid := h.main[id] == entry.win && (!show || !h.startHiddenLocked(id)) && !h.quitting
				h.mu.Unlock()
				if valid && native.IsNativeWindow(uintptr(entry.win.NativeWindow())) {
					_ = native.SetWindowAlpha(uintptr(entry.win.NativeWindow()), byte(math.Round(entry.opacity*amount*255)))
				}
			}
		})
		h.mu.Lock()
		quitting := h.quitting
		h.mu.Unlock()
		if t >= 1 || quitting {
			return
		}
		time.Sleep(16 * time.Millisecond)
	}
}

func (h *Desktop) hidePeekLocked() {
	h.mu.Lock()
	if !h.peekActive {
		h.mu.Unlock()
		return
	}
	h.peekTransition = true
	entries := h.peekWindows
	previous := h.peekPrevious
	input := h.peekInput
	if h.peekFocusTimer != nil {
		h.peekFocusTimer.Stop()
	}
	h.mu.Unlock()
	if input != nil {
		input.Active(false)
	}
	foreground := native.ForegroundWindow()
	restoreFocus := false
	for _, entry := range entries {
		if native.OwnsWindow(uintptr(entry.win.NativeWindow()), foreground) {
			restoreFocus = true
			break
		}
	}
	h.animatePeek(entries, false)
	g, _, _, _ := h.Service.Store.Snapshot()
	application.InvokeSync(func() {
		for id, entry := range entries {
			h.mu.Lock()
			valid := h.main[id] == entry.win
			hidden := h.hidden || h.startHiddenLocked(id)
			quitting := h.quitting
			h.mu.Unlock()
			hwnd := uintptr(entry.win.NativeWindow())
			if !valid || !native.IsNativeWindow(hwnd) {
				continue
			}
			if err := native.RestoreDesktopWindow(hwnd, entry.placement); err != nil {
				slog.Warn("恢复桌面窗口", "error", err)
				continue
			}
			opacity := entry.opacity
			if w, err := h.Service.Store.Window(id); err == nil {
				opacity = appearance.Resolve(g, w, shell.SystemDark()).Opacity
			}
			_ = native.SetOpacity(hwnd, opacity)
			h.mu.Lock()
			h.desktopParents[id] = native.DesktopParent(hwnd)
			h.desktopReady[id] = native.IsDesktopAttached(hwnd)
			h.mu.Unlock()
			if !hidden && !quitting {
				native.ShowInactive(hwnd)
			} else {
				entry.win.Hide()
			}
		}
		h.mu.Lock()
		active := entries[h.activeMain]
		current := active.win != nil && h.main[h.activeMain] == active.win && !h.startHiddenLocked(h.activeMain)
		h.mu.Unlock()
		if current && native.IsDesktopAttached(uintptr(active.win.NativeWindow())) {
			_ = native.RaiseDesktopWindow(uintptr(active.win.NativeWindow()))
		}
	})
	h.mu.Lock()
	h.peekActive = false
	h.peekTransition = false
	h.peekHadFocus = false
	h.peekWindows = nil
	quitting := h.quitting
	h.mu.Unlock()
	if restoreFocus && !quitting && native.IsNativeWindow(previous) {
		application.InvokeSync(func() { native.FocusPeekWindow(previous) })
	}
	for id := range entries {
		h.scheduleGeometry(id)
	}
	// A display may have changed while these HWNDs were temporarily foreground.
	application.InvokeAsync(h.correctAll)
}

func (h *Desktop) peekMouseClick(x, y int32) {
	h.mu.Lock()
	if !h.peekActive || h.peekTransition {
		h.mu.Unlock()
		return
	}
	handles := []uintptr{}
	for id, entry := range h.peekWindows {
		if h.main[id] == entry.win && !h.startHiddenLocked(id) {
			handles = append(handles, uintptr(entry.win.NativeWindow()))
		}
	}
	h.mu.Unlock()
	hit := native.WindowAt(x, y)
	if native.MenuActive() && native.IsMenuWindow(hit) {
		return
	}
	for _, hwnd := range handles {
		if native.OwnsWindow(hwnd, hit) {
			return
		}
	}
	h.hidePeek()
}

func (h *Desktop) schedulePeekFocusCheck() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.peekActive || h.quitting {
		return
	}
	if h.peekFocusTimer != nil {
		h.peekFocusTimer.Stop()
	}
	h.peekFocusTimer = time.AfterFunc(150*time.Millisecond, h.checkPeekFocus)
}
func (h *Desktop) checkPeekFocus() {
	h.mu.Lock()
	if !h.peekActive || h.quitting {
		h.mu.Unlock()
		return
	}
	transition := h.peekTransition
	hadFocus := h.peekHadFocus
	handles := []uintptr{}
	for id, entry := range h.peekWindows {
		if h.main[id] == entry.win && !h.startHiddenLocked(id) {
			handles = append(handles, uintptr(entry.win.NativeWindow()))
		}
	}
	h.mu.Unlock()
	if transition || native.MenuActive() {
		h.schedulePeekFocusCheck()
		return
	}
	foreground := native.ForegroundWindow()
	for _, hwnd := range handles {
		if native.OwnsWindow(hwnd, foreground) {
			h.mu.Lock()
			h.peekHadFocus = true
			h.mu.Unlock()
			return
		}
	}
	// Windows can deny foreground activation. A group that never acquired focus
	// has not lost it; keep its topmost windows available until clicked/dismissed.
	if hadFocus {
		h.hidePeek()
	}
}
