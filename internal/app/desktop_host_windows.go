//go:build windows

package app

import (
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"strings"
	"time"

	"deskgrid/internal/appearance"
	"deskgrid/internal/model"
	"deskgrid/internal/shell"
	native "deskgrid/internal/windows"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type nativeWindowState struct {
	initialized     bool
	title           string
	collapsed       bool
	collapsedHeight int
	opacity         float64
	cornerRadius    float64
	positionLocked  bool
	sizeAnimation   *windowSizeAnimation
}

// A token prevents a cancelled fade from changing a later showing of the same
// HWND. Apply updates its target while the animation owns native opacity.
type windowFade struct{ opacity float64 }

// Apply changes native properties only when their effective values change.
// Entry changes and background refreshes must not resize or recompose the HWND.
func (h *Desktop) Apply(w model.Window) {
	h.mu.Lock()
	win := h.main[w.ID]
	ready := h.nativeReady[w.ID]
	h.mu.Unlock()
	if win == nil || !ready {
		return
	}
	g, _, _, _ := h.Service.Store.Snapshot()
	a := appearance.Resolve(g, w, shell.SystemDark())
	var applyErr error
	var titleChanged bool
	var startAnimation *windowSizeAnimation
	application.InvokeSync(func() {
		h.mu.Lock()
		current := h.main[w.ID] == win
		previous := h.applied[w.ID]
		pixelRatio := h.pixelRatios[w.ID]
		transition := h.peekTransition
		fade := h.showing[w.ID]
		if fade != nil {
			fade.opacity = a.Opacity
		}
		h.mu.Unlock()
		if !current {
			return
		}
		hwnd := uintptr(win.NativeWindow())
		if !native.IsNativeWindow(hwnd) {
			return
		}
		titleChanged = !previous.initialized || previous.title != w.Title
		if titleChanged {
			win.SetTitle(w.Title)
		}
		sizeChanged := false
		animation := previous.sizeAnimation
		if !previous.initialized || previous.collapsed != w.Collapsed || (w.Collapsed && previous.collapsedHeight != model.CollapsedWindowHeight(w)) {
			width, height := win.Size()
			targetWidth, targetHeight := max(220, width), model.CollapsedWindowHeight(w)
			// Keep the expanded minimum from clamping intermediate animation frames.
			win.SetMinSize(220, 24)
			if !w.Collapsed {
				targetWidth = max(220, int(w.ExpandedSize.Width))
				targetHeight = max(120, int(w.ExpandedSize.Height))
				if !previous.initialized {
					targetWidth = max(220, int(w.Bounds.Width))
					targetHeight = max(120, int(w.Bounds.Height))
				}
				// Fit the current display without replacing preferred JSON bounds.
				_, _, targetWidth, targetHeight = native.Clamp(int(w.Bounds.X), int(w.Bounds.Y), targetWidth, targetHeight, h.workAreas())
			}
			if width != targetWidth || height != targetHeight {
				if previous.initialized && !g.ReducedMotion && !shell.ReducedMotion() {
					animation = &windowSizeAnimation{fromWidth: width, fromHeight: height, width: targetWidth, height: targetHeight}
					startAnimation = animation
				} else {
					animation = nil
					if applyErr = native.ResizeDesktopWindow(hwnd, targetWidth, targetHeight); applyErr != nil {
						return
					}
				}
				sizeChanged = true
			} else {
				animation = nil
			}
			// Resize first: Wails otherwise clamps 32 to 120 before we can restore ExpandedSize.
			if !w.Collapsed && animation == nil {
				win.SetMinSize(220, 120)
			}
		}
		if !previous.initialized || previous.positionLocked != w.PositionLocked || (previous.sizeAnimation == nil) != (animation == nil) {
			win.SetResizable(!w.PositionLocked && animation == nil)
		}
		if (!previous.initialized || previous.opacity != a.Opacity || sizeChanged) && !transition && fade == nil {
			if applyErr = native.SetOpacity(hwnd, a.Opacity); applyErr != nil {
				return
			}
		}
		clipHeight := 0
		if w.Collapsed && animation == nil {
			clipHeight = model.CollapsedWindowHeight(w)
		}
		if sizeChanged || !previous.initialized || previous.cornerRadius != a.CornerRadius {
			if applyErr = native.RoundWindow(hwnd, clipHeight, pixelRatio, a.CornerRadius); applyErr != nil {
				return
			}
		}
		h.mu.Lock()
		if h.main[w.ID] == win {
			h.applied[w.ID] = nativeWindowState{initialized: true, title: w.Title, collapsed: w.Collapsed, collapsedHeight: model.CollapsedWindowHeight(w), opacity: a.Opacity, cornerRadius: a.CornerRadius, positionLocked: w.PositionLocked, sizeAnimation: animation}
		}
		h.mu.Unlock()
	})
	if applyErr != nil {
		h.notifyError(applyErr)
	} else if startAnimation != nil {
		go h.animateWindowSize(w.ID, win, startAnimation)
	}
	if titleChanged {
		go h.rebuildTray()
	}
}

// CurrentBounds is used before persisting a collapse transition, so a pending
// geometry debounce cannot lose the window's most recent expanded dimensions.
func (h *Desktop) CurrentBounds(id string) (model.Bounds, bool) {
	h.mu.Lock()
	win := h.main[id]
	ready := h.nativeReady[id]
	animating := h.applied[id].sizeAnimation != nil
	var change geometryEdit
	edit := h.geometryEdits[id]
	if edit != nil {
		change = *edit
	}
	h.mu.Unlock()
	if win == nil || !ready || animating || !native.IsNativeWindow(uintptr(win.NativeWindow())) {
		return model.Bounds{}, false
	}
	w, err := h.Service.Store.Window(id)
	if err != nil {
		return model.Bounds{}, false
	}
	if edit == nil {
		return w.Bounds, true // Keep the preferred position/size while display-clamped.
	}
	if change.active {
		b := win.Bounds()
		change.after = model.Bounds{X: float64(b.X), Y: float64(b.Y), Width: float64(b.Width), Height: float64(b.Height), Unit: "dip"}
	}
	return geometryBounds(w, change), true
}

func (h *Desktop) setMainBounds(win *application.WebviewWindow, x, y, width, height int) {
	screen, _ := win.GetScreen()
	for _, candidate := range h.App.Screen.GetAll() {
		b := candidate.Bounds
		if x >= b.X && x < b.X+b.Width && y >= b.Y && y < b.Y+b.Height {
			screen = candidate
			break
		}
	}
	px, py, pw, ph := x, y, width, height
	if screen != nil {
		scale := float64(screen.ScaleFactor)
		if scale <= 0 {
			scale = 1
		}
		px = screen.PhysicalBounds.X + int(math.Round(float64(x-screen.Bounds.X)*scale))
		py = screen.PhysicalBounds.Y + int(math.Round(float64(y-screen.Bounds.Y)*scale))
		pw = int(math.Round(float64(width) * scale))
		ph = int(math.Round(float64(height) * scale))
	}
	var err error
	application.InvokeSync(func() { err = native.SetDesktopBounds(uintptr(win.NativeWindow()), px, py, pw, ph, false) })
	if err != nil {
		h.notifyError(err)
	}
}

func (h *Desktop) showMain(id string, win *application.WebviewWindow) {
	g, _, _, _ := h.Service.Store.Snapshot()
	w, err := h.Service.Store.Window(id)
	if err != nil {
		return
	}
	a := appearance.Resolve(g, w, shell.SystemDark())
	h.mu.Lock()
	if h.peekActive || h.peekTransition {
		h.mu.Unlock()
		return
	}
	allowed := h.main[id] == win && h.nativeReady[id] && h.surfaceReady[id] && h.desktopReady[id] && !h.hidden && !h.startHiddenLocked(id) && !h.quitting
	showing := h.showing[id] != nil
	var fade *windowFade
	if allowed && !showing {
		fade = &windowFade{opacity: a.Opacity}
		h.showing[id] = fade
	}
	h.mu.Unlock()
	if !allowed || !native.IsDesktopAttached(uintptr(win.NativeWindow())) {
		if allowed && !showing {
			h.mu.Lock()
			if h.showing[id] == fade {
				delete(h.showing, id)
			}
			h.mu.Unlock()
		}
		return
	}
	win.ExecJS("document.documentElement.classList.remove('global-hiding')")
	if showing {
		return
	}
	if win.IsVisible() {
		application.InvokeSync(func() {
			h.mu.Lock()
			current := h.showing[id] == fade && h.main[id] == win && !h.hidden && !h.startHiddenLocked(id)
			opacity := fade.opacity
			if h.showing[id] == fade {
				delete(h.showing, id)
			}
			h.mu.Unlock()
			if current {
				_ = native.SetOpacity(uintptr(win.NativeWindow()), opacity)
			}
		})
		return
	}
	go h.fadeShow(id, win, a.Opacity, g.ReducedMotion || shell.ReducedMotion(), fade)
}

func surfaceColour(value string) application.RGBA {
	rgb, _ := strconv.ParseUint(strings.TrimPrefix(value, "#"), 16, 32)
	return application.NewRGB(uint8(rgb>>16), uint8(rgb>>8), uint8(rgb))
}

func (h *Desktop) clipWindow(id string, win *application.WebviewWindow) {
	application.InvokeAsync(func() {
		h.mu.Lock()
		pixelRatio := h.pixelRatios[id]
		state := h.applied[id]
		h.mu.Unlock()
		if state.sizeAnimation != nil {
			return // The animation owns clipping; ignore delayed resize events.
		}
		height := 0
		radius := 7.0
		if state.initialized {
			radius = state.cornerRadius
		}
		if state.collapsed {
			height = state.collapsedHeight
		}
		if native.IsNativeWindow(uintptr(win.NativeWindow())) {
			if err := native.RoundWindow(uintptr(win.NativeWindow()), height, pixelRatio, radius); err != nil {
				slog.Warn("窗口圆角", "error", err)
			}
		}
	})
}

func (h *Desktop) focusAuxiliary(id string, win *application.WebviewWindow) {
	h.mu.Lock()
	ready := h.surfaceReady[id]
	h.mu.Unlock()
	if ready {
		win.Show()
		win.Focus()
	}
}

func (h *Desktop) Interact(id string) {
	h.mu.Lock()
	win := h.main[id]
	allowed := win != nil && h.desktopReady[id] && h.surfaceReady[id] && (!h.hidden || h.peekActive) && !h.startHiddenLocked(id) && !h.quitting
	if allowed {
		h.activeMain = id
	}
	others := []*application.WebviewWindow{}
	for otherID, w := range h.main {
		if otherID != id {
			others = append(others, w)
		}
	}
	h.mu.Unlock()
	if !allowed {
		return
	}
	application.InvokeAsync(func() {
		h.mu.Lock()
		current := h.main[id] == win && (!h.hidden || h.peekActive) && !h.startHiddenLocked(id) && !h.quitting
		peeking := h.peekActive && h.peekWindows[id].win == win
		transition := h.peekTransition
		h.mu.Unlock()
		if !current || !native.IsNativeWindow(uintptr(win.NativeWindow())) {
			return
		}
		if peeking && native.OwnsWindow(uintptr(win.NativeWindow()), native.ForegroundWindow()) {
			h.mu.Lock()
			h.peekHadFocus = true
			h.mu.Unlock()
		}
		if transition {
			return
		} else if peeking {
			if err := native.RaisePeekWindow(uintptr(win.NativeWindow())); err != nil {
				slog.Warn("临时前置窗口", "error", err)
				return
			}
		} else if err := native.RaiseDesktopWindow(uintptr(win.NativeWindow())); err != nil {
			slog.Warn("桌面窗口前后顺序", "error", err)
			return
		}
		for _, other := range others {
			other.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:focus',{detail:false}));window.dispatchEvent(new Event('deskgrid:dismiss-menu'));")
		}
		win.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:focus',{detail:true}));")
	})
}

func (h *Desktop) ContextMenu(id string, actions []model.MenuAction, x, y float64) (string, error) {
	w, err := h.Service.Store.Window(id)
	if err != nil {
		return "", err
	}
	g, _, _, _ := h.Service.Store.Snapshot()
	a := appearance.Resolve(g, w, shell.SystemDark())
	h.mu.Lock()
	win := h.main[id]
	ratio := h.pixelRatios[id]
	h.mu.Unlock()
	if win == nil {
		return "", nil
	}
	if ratio <= 0 {
		ratio = 1
	}
	items := []native.PopupItem{}
	ids := []string{}
	previous := ""
	for i, action := range actions {
		if i > 0 && action.Group != previous {
			items = append(items, native.PopupItem{Separator: true})
			ids = append(ids, "")
		}
		items = append(items, native.PopupItem{Text: action.Name, Enabled: action.Enabled})
		ids = append(ids, action.ID)
		previous = action.Group
	}
	selected := -1
	application.InvokeSync(func() {
		h.mu.Lock()
		entry := h.peekWindows[id]
		peeking := h.peekActive && entry.win == win
		h.mu.Unlock()
		if native.IsDesktopAttached(uintptr(win.NativeWindow())) || peeking {
			selected, err = native.ContextMenu(uintptr(win.NativeWindow()), items, x, y, ratio, native.PopupStyle{Background: a.BackgroundColor, Text: a.TextColor, Opacity: a.Opacity})
		}
	})
	if err != nil || selected < 0 || selected >= len(ids) {
		return "", err
	}
	return ids[selected], nil
}

// Runtime-ready precedes Vue's first content paint. The frontend explicitly
// releases each hidden window only after data, layout and fonts are ready.
func (h *Desktop) SurfaceReady(id string, pixelRatio float64) {
	h.mu.Lock()
	if strings.HasPrefix(id, "prepared-main-") {
		if h.prepared != nil && h.preparedID == id {
			h.preparedReady = true
		}
		h.mu.Unlock()
		return
	}
	wasReady := h.surfaceReady[id]
	win := h.main[id]
	if win == nil {
		key := strings.TrimPrefix(id, "settings-")
		if id == "pin-chooser" {
			key = "pin"
		}
		win = h.settings[key]
	}
	if win != nil {
		h.surfaceReady[id] = true
		h.pixelRatios[id] = pixelRatio
	}
	h.mu.Unlock()
	if win == nil {
		return
	}
	h.clipWindow(id, win)
	if wasReady {
		return
	}
	h.mu.Lock()
	main := h.main[id] == win
	h.mu.Unlock()
	if main {
		h.mount(id)
		return
	}
	g, _, _, _ := h.Service.Store.Snapshot()
	go h.fadeShow(id, win, 1, g.ReducedMotion || shell.ReducedMotion(), nil)
	win.OnWindowEvent(events.Common.WindowDidResize, func(e *application.WindowEvent) { h.clipWindow(id, win) })
}

func (h *Desktop) fadeShow(id string, win *application.WebviewWindow, opacity float64, reduced bool, fade *windowFade) {
	if fade != nil {
		defer func() {
			h.mu.Lock()
			if h.showing[id] == fade {
				delete(h.showing, id)
			}
			h.mu.Unlock()
		}()
	}
	valid := func() bool {
		h.mu.Lock()
		defer h.mu.Unlock()
		if h.quitting {
			return false
		}
		if fade != nil {
			return h.main[id] == win && h.showing[id] == fade && !h.hidden && !h.startHiddenLocked(id) && h.desktopReady[id] && !h.peekActive && !h.peekTransition
		}
		key := strings.TrimPrefix(id, "settings-")
		if id == "pin-chooser" {
			key = "pin"
		}
		return h.settings[key] == win
	}
	targetOpacity := func() float64 {
		if fade == nil {
			return opacity
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		return fade.opacity
	}
	if !valid() {
		return
	}
	application.InvokeSync(func() {
		if !valid() || !native.IsNativeWindow(uintptr(win.NativeWindow())) {
			return
		}
		if !reduced {
			_ = native.SetWindowAlpha(uintptr(win.NativeWindow()), 0)
		} else {
			_ = native.SetOpacity(uintptr(win.NativeWindow()), targetOpacity())
		}
		// Wails also restores WebView composition and its input surface here;
		// showing only the HWND would leave a previously hidden WebView blank.
		win.Show()
	})
	if !reduced {
		start := time.Now()
		ticker := time.NewTicker(16 * time.Millisecond)
		defer ticker.Stop()
		for range ticker.C {
			if !valid() {
				return
			}
			t := min(1, float64(time.Since(start))/float64(200*time.Millisecond))
			amount := t * t * (3 - 2*t)
			application.InvokeSync(func() {
				if valid() && native.IsNativeWindow(uintptr(win.NativeWindow())) {
					_ = native.SetWindowAlpha(uintptr(win.NativeWindow()), byte(math.Round(targetOpacity()*amount*255)))
				}
			})
			if t >= 1 {
				break
			}
		}
	}
	application.InvokeSync(func() {
		if valid() && native.IsNativeWindow(uintptr(win.NativeWindow())) {
			_ = native.SetOpacity(uintptr(win.NativeWindow()), targetOpacity())
			if fade != nil {
				// Release ownership on the UI thread with the final frame so an
				// Apply immediately afterwards cannot lose its new opacity.
				h.mu.Lock()
				if h.showing[id] == fade {
					delete(h.showing, id)
				}
				h.mu.Unlock()
			}
		}
	})
}

func (h *Desktop) mount(id string) {
	h.mu.Lock()
	if h.peekActive || h.peekTransition {
		h.mu.Unlock()
		go h.joinPeek(id)
		return
	}
	win := h.main[id]
	ready := h.nativeReady[id]
	h.mu.Unlock()
	if win == nil || !ready {
		return
	}
	attached, join := false, false
	application.InvokeSync(func() {
		// A queued mount may run after peek has begun. Recheck on the same UI
		// thread as the reparent operation before changing visibility or parent.
		h.mu.Lock()
		current := h.main[id] == win && h.nativeReady[id] && !h.quitting
		join = h.peekActive || h.peekTransition
		h.mu.Unlock()
		if !current || join {
			return
		}
		hwnd := uintptr(win.NativeWindow())
		if !native.IsNativeWindow(hwnd) {
			return
		}
		var err error
		if !native.IsDesktopAttached(hwnd) {
			// Keep an unattached normal surface hidden, including on failure.
			if win.IsVisible() {
				win.Hide()
			}
			err = native.AttachDesktop(hwnd)
		}
		h.recordDesktopMount(id, hwnd, err)
		attached = err == nil
	})
	if join {
		go h.joinPeek(id)
	} else if attached {
		h.showMain(id, win)
	}
}

func (h *Desktop) recordDesktopMount(id string, hwnd uintptr, err error) {
	h.mu.Lock()
	if h.main[id] == nil {
		h.mu.Unlock()
		return
	}
	previousError := h.mountErrors[id]
	h.desktopReady[id] = err == nil
	if err == nil {
		h.desktopParents[id] = native.DesktopParent(hwnd)
		delete(h.mountErrors, id)
	} else {
		h.mountErrors[id] = err.Error()
	}
	attached := 0
	for _, ready := range h.desktopReady {
		if ready {
			attached++
		}
	}
	status := fmt.Sprintf("仅在桌面显示（%d 个已挂载）", attached)
	if len(h.mountErrors) > 0 {
		reason := err
		if reason == nil {
			for _, message := range h.mountErrors {
				reason = fmt.Errorf("%s", message)
				break
			}
		}
		status = fmt.Sprintf("%d 个已挂载，%d 个等待重试：%s", attached, len(h.mountErrors), reason)
	}
	changed := h.status["desktop"] != status
	h.status["desktop"] = status
	h.mu.Unlock()
	if err != nil && previousError != err.Error() {
		slog.Warn("桌面挂载暂不可用，主内容窗口保持隐藏", "error", err)
	}
	if changed {
		label := "DeskGrid · " + status
		if len([]rune(label)) > 100 {
			label = string([]rune(label)[:100])
		}
		h.tray.SetTooltip(label)
	}
}

// Explorer can destroy desktop children when it restarts. Recreate only windows
// still in the persisted open set; a pending recovery must not reopen a user-closed window.
func (h *Desktop) restoreDesktopWindow(id string) {
	h.Service.mu.Lock()
	defer h.Service.mu.Unlock()
	h.mu.Lock()
	quitting := h.quitting
	current := h.main[id]
	hidden := h.hidden
	h.mu.Unlock()
	if quitting {
		return
	}
	if current != nil && native.IsNativeWindow(uintptr(current.NativeWindow())) {
		return
	}
	_, session, _, _ := h.Service.Store.Snapshot()
	opened := false
	for _, openID := range session.OpenWindowIDs {
		if openID == id {
			opened = true
			break
		}
	}
	if !opened {
		return
	}
	definition, err := h.Service.Store.Window(id)
	if err != nil {
		return
	}
	h.mu.Lock()
	delete(h.main, id)
	delete(h.nativeReady, id)
	delete(h.surfaceReady, id)
	delete(h.pixelRatios, id)
	delete(h.showing, id)
	delete(h.desktopReady, id)
	delete(h.applied, id)
	delete(h.geometryEdits, id)
	delete(h.desktopParents, id)
	delete(h.mountErrors, id)
	h.mu.Unlock()
	if err = h.Open(definition); err != nil {
		slog.Error("恢复桌面窗口失败", "error", err)
	}
	h.mu.Lock()
	h.hidden = hidden
	h.mu.Unlock()
}
