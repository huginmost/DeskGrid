//go:build windows

package app

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"deskgrid/frontend"
	"deskgrid/internal/appearance"
	"deskgrid/internal/branding"
	"deskgrid/internal/config"
	"deskgrid/internal/model"
	"deskgrid/internal/shell"
	native "deskgrid/internal/windows"
	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

type Desktop struct {
	App              *application.App
	Service          *Service
	mu               sync.Mutex
	main             map[string]*application.WebviewWindow
	settings         map[string]*application.WebviewWindow
	timers           map[string]*time.Timer
	geometryEdits    map[string]*geometryEdit
	tray             *application.SystemTray
	hidden           bool
	quitting         bool
	status           map[string]string
	desktopReady     map[string]bool
	nativeReady      map[string]bool
	surfaceReady     map[string]bool
	pixelRatios      map[string]float64
	prepared         *application.WebviewWindow
	preparedID       string
	preparedReady    bool
	preparing        bool
	preparedSequence uint64
	showing          map[string]*windowFade
	startID          string
	hideStartWindow  bool
	alignment        native.AlignmentGuides
	applied          map[string]nativeWindowState
	desktopParents   map[string]uintptr
	mountErrors      map[string]string
	done             chan struct{}
	peekMu           sync.Mutex
	peekActive       bool
	peekTransition   bool
	peekHadFocus     bool
	peekWindows      map[string]peekWindow
	peekPrevious     uintptr
	peekFocusTimer   *time.Timer
	activeMain       string
	peekInput        *native.PeekInput
}

func NewDesktop(store *config.Store, configDir string) *Desktop {
	h := &Desktop{main: map[string]*application.WebviewWindow{}, settings: map[string]*application.WebviewWindow{}, timers: map[string]*time.Timer{}, status: map[string]string{"desktop": "正在挂载桌面…", "hotkey": "未注册"}, desktopReady: map[string]bool{}, nativeReady: map[string]bool{}, surfaceReady: map[string]bool{}, pixelRatios: map[string]float64{}, showing: map[string]*windowFade{}, applied: map[string]nativeWindowState{}, desktopParents: map[string]uintptr{}, mountErrors: map[string]string{}, done: make(chan struct{})}
	h.Service = NewService(store, h)
	_, _, savedWindows, _ := store.Snapshot()
	for _, w := range savedWindows {
		if w.Type == "start" {
			h.startID = w.ID
		}
		if w.Type != "collection" {
			continue
		}
		name := w.Title
		for _, previousName := range []string{"综合窗口", "收纳窗口"} {
			if name == previousName {
				w.Title = "应用窗口"
			} else if suffix, ok := strings.CutPrefix(name, previousName+" - "); ok {
				if _, err := strconv.Atoi(suffix); err == nil {
					w.Title = "应用窗口 - " + suffix
				}
			}
		}
		if w.Title != name {
			if err := store.SaveWindow(w); err != nil {
				slog.Error("更新窗口默认名称失败", "error", err)
			}
		}
	}
	// Old configurations may have enabled the former ordinary-window fallback.
	// Main surfaces now always belong to Explorer's desktop; keep the legacy field true.
	global, _, _, _ := store.Snapshot()
	h.hideStartWindow = global.HideStartWindow
	legacyPeek := global.PeekHotkey == nil || strings.EqualFold(strings.TrimSpace(model.PeekBinding(global)), "Win")
	if !global.DesktopMode || legacyPeek {
		global.DesktopMode = true
		if legacyPeek {
			key := "Win+A"
			global.PeekHotkey = &key
		}
		if e := store.SaveGlobal(global); e != nil {
			slog.Error("保存桌面显示模式失败", "error", e)
		}
	}
	h.Service.Notify = func(id string) {
		h.mu.Lock()
		w := h.main[id]
		h.mu.Unlock()
		if w != nil {
			w.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:changed')); ")
		}
	}
	h.App = application.New(application.Options{Name: branding.Name, Description: "Windows 桌面文件与快捷入口整理工具", Icon: branding.Icon, LogLevel: slog.LevelWarn,
		Assets: application.AssetOptions{Handler: application.BundledAssetFileServer(frontend.Assets()), Middleware: func(next http.Handler) http.Handler { return h.Service.Handler(next) }, DisableLogging: true},
		Windows: application.WindowsOptions{DisableQuitOnLastWindowClosed: true, WebviewUserDataPath: filepath.Join(configDir, "webview"), WndProcInterceptor: func(hwnd uintptr, msg uint32, wparam, lparam uintptr) (uintptr, bool) {
			if result, handled := h.alignmentMessage(hwnd, msg, wparam, lparam); handled {
				return result, true
			}
			if result, handled := native.PeekMessage(hwnd, msg, wparam, lparam); handled {
				return result, true
			}
			if result, handled := native.MenuMessage(hwnd, msg, lparam); handled {
				return result, true
			}
			if msg == 0x7e || msg == 0x2e0 || msg == 0x1a {
				// Run after Wails has refreshed its display/DPI cache.
				go application.InvokeAsync(h.correctAll)
			}
			return 0, false
		}},
		OnShutdown: h.flush, ErrorHandler: func(e error) {
			slog.Error("Wails", "error", e)
			h.mu.Lock()
			h.status["hotkey"] = e.Error()
			h.mu.Unlock()
		},
		ShouldQuit: func() bool { return !h.Service.Active() },
	})
	h.tray = h.App.SystemTray.New()
	h.tray.SetIcon(branding.TrayLight)
	h.tray.SetDarkModeIcon(branding.TrayDark)
	h.tray.SetTooltip("DeskGrid · 桌面整理")
	h.tray.OnClick(func() { h.Activate(nil) })
	h.rebuildTray()
	h.App.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(e *application.ApplicationEvent) {
		go func() {
			g, _, _, _ := store.Snapshot()
			if g.Hotkey != "" {
				err := h.App.GlobalShortcut.Register(g.Hotkey, h.Toggle)
				h.mu.Lock()
				if err != nil {
					h.status["hotkey"] = "注册失败：" + err.Error()
				} else {
					h.status["hotkey"] = "已注册 " + g.Hotkey
				}
				h.mu.Unlock()
			}
			if err := h.changePeekHotkey(model.PeekBinding(g), ""); err != nil {
				h.mu.Lock()
				h.status["peekHotkey"] = "注册失败：" + err.Error()
				h.mu.Unlock()
				slog.Warn("临时前置快捷键", "error", err)
			}
			go h.maintainDesktop()
			go h.prepareMainWindow()
		}()
	})
	return h
}
func (h *Desktop) Restore() error {
	if err := h.Service.reconcileFolderTitles(); err != nil {
		return err
	}
	_, session, windows, _ := h.Service.Store.Snapshot()
	count := 0
	for _, id := range session.OpenWindowIDs {
		for _, w := range windows {
			if w.ID == id {
				if e := h.Open(w); e != nil {
					return e
				}
				count++
			}
		}
	}
	if count == 0 {
		for _, w := range windows {
			if w.Type == "start" {
				if e := h.Open(w); e != nil {
					return e
				}
				return h.Service.setOpen(w.ID, true)
			}
		}
	}
	return nil
}
func (h *Desktop) Open(w model.Window) error {
	h.mu.Lock()
	if w.Type == "start" {
		h.startID = w.ID
	}
	existing := h.main[w.ID]
	if !h.peekActive && !h.peekTransition {
		h.hidden = false
	}
	h.mu.Unlock()
	if existing != nil {
		h.mount(w.ID)
		h.showMain(w.ID, existing)
		return nil
	}
	if w.Type == "folder" {
		if _, e := os.Stat(w.CurrentPath); e != nil {
			if _, rootErr := os.Stat(w.RootPath); rootErr == nil {
				w.CurrentPath = w.RootPath
				if w.TitleMode != "custom" {
					w.Title = folderTitle(w.RootPath)
				}
				if e = h.Service.Store.SaveWindow(w); e != nil {
					return e
				}
			}
		}
	}
	height := int(w.Bounds.Height)
	if w.Collapsed {
		height = model.CollapsedWindowHeight(w)
	}
	x, y, width, height := native.Clamp(int(w.Bounds.X), int(w.Bounds.Y), int(w.Bounds.Width), height, h.workAreas())
	g, _, _, _ := h.Service.Store.Snapshot()
	a := appearance.Resolve(g, w, shell.SystemDark())
	h.mu.Lock()
	win := h.prepared
	reused := win != nil && h.preparedReady
	if reused {
		h.prepared = nil
		h.preparedID = ""
		h.preparedReady = false
	}
	h.mu.Unlock()
	if !reused {
		win = h.App.Window.NewWithOptions(application.WebviewWindowOptions{Name: w.ID, Title: w.Title, URL: "/?window=" + url.QueryEscape(w.ID), Width: width, Height: height, X: x, Y: y, InitialPosition: application.WindowXY, MinWidth: 220, MinHeight: 32, Hidden: true, Frameless: true, EnableFileDrop: true, BackgroundColour: surfaceColour(a.BackgroundColor), DefaultContextMenuDisabled: true, DevToolsEnabled: false, Windows: application.WindowsWindow{HiddenOnTaskbar: true, DisableFramelessWindowDecorations: true, WindowDidMoveDebounceMS: 300}})
	} else {
		win.SetTitle(w.Title)
		win.SetPosition(x, y)
		win.SetSize(width, height)
		win.SetBackgroundColour(surfaceColour(a.BackgroundColor))
	}
	h.mu.Lock()
	h.main[w.ID] = win
	if reused {
		h.nativeReady[w.ID] = true
	}
	h.mu.Unlock()
	win.OnWindowEvent(events.Common.WindowRuntimeReady, func(e *application.WindowEvent) {
		h.mu.Lock()
		if h.main[w.ID] != win {
			h.mu.Unlock()
			return
		}
		h.nativeReady[w.ID] = true
		h.mu.Unlock()
		if latest, err := h.Service.Store.Window(w.ID); err == nil {
			h.Apply(latest)
			if reused {
				h.assignPrepared(win, latest)
			}
		}
		h.mount(w.ID)
	})
	win.OnWindowEvent(events.Common.WindowDidMove, func(e *application.WindowEvent) { h.scheduleGeometry(w.ID) })
	win.OnWindowEvent(events.Common.WindowDidResize, func(e *application.WindowEvent) { h.clipWindow(w.ID, win); h.scheduleGeometry(w.ID) })
	win.OnWindowEvent(events.Common.WindowFocus, func(e *application.WindowEvent) {
		h.Interact(w.ID)
	})
	win.OnWindowEvent(events.Common.WindowLostFocus, func(e *application.WindowEvent) {
		win.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:focus',{detail:false}));")
		h.schedulePeekFocusCheck()
	})
	win.RegisterHook(events.Common.WindowClosing, func(e *application.WindowEvent) {
		h.mu.Lock()
		protected := !h.quitting && h.main[w.ID] == win
		parent := h.desktopParents[w.ID]
		h.mu.Unlock()
		if protected && !(parent != 0 && !native.IsNativeWindow(parent)) {
			if latest, err := h.Service.Store.Window(w.ID); err == nil && (latest.Type == "start" || latest.PositionLocked) {
				e.Cancel()
			}
		}
	})
	win.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		application.InvokeSync(func() { h.alignment.Clear() })
		h.mu.Lock()
		quitting := h.quitting
		current := h.main[w.ID] == win
		parent := h.desktopParents[w.ID]
		desktopLost := current && parent != 0 && !native.IsNativeWindow(parent)
		if current {
			delete(h.main, w.ID)
			delete(h.desktopReady, w.ID)
			delete(h.nativeReady, w.ID)
			delete(h.surfaceReady, w.ID)
			delete(h.pixelRatios, w.ID)
			delete(h.showing, w.ID)
			delete(h.applied, w.ID)
			delete(h.geometryEdits, w.ID)
			delete(h.desktopParents, w.ID)
			delete(h.mountErrors, w.ID)
		}
		h.mu.Unlock()
		if !quitting && desktopLost {
			go h.restoreDesktopWindow(w.ID)
			return
		}
		if !quitting && current {
			go func() {
				h.Service.mu.Lock()
				h.mu.Lock()
				reopened := h.main[w.ID] != nil
				h.mu.Unlock()
				var err error
				if !reopened {
					err = h.Service.removeWindow(w.ID)
				}
				h.Service.mu.Unlock()
				if err != nil {
					h.notifyError(err)
				}
				h.rebuildTray()
			}()
		}
	})
	win.OnWindowEvent(events.Common.WindowFilesDropped, func(e *application.WindowEvent) {
		ctrl, shift := shell.Modifiers()
		detail := map[string]any{"windowId": w.ID, "paths": e.Context().DroppedFiles(), "ctrl": ctrl, "shift": shift, "x": 0, "y": 0}
		if d := e.Context().DropTargetDetails(); d != nil {
			detail["x"] = d.X
			detail["y"] = d.Y
			detail["attributes"] = d.Attributes
		}
		b, _ := json.Marshal(detail)
		win.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:drop',{detail:" + string(b) + "}));")
	})
	win.OnWindowEvent(events.Windows.WindowDragLeave, func(e *application.WindowEvent) {
		win.ExecJS("window.dispatchEvent(new CustomEvent('deskgrid:drag-leave'));")
	})
	if reused {
		h.Apply(w)
		h.mount(w.ID)
		h.assignPrepared(win, w)
	}
	go h.prepareMainWindow()
	go h.rebuildTray()
	return nil
}
func (h *Desktop) Close(id string) {
	h.mu.Lock()
	w := h.main[id]
	delete(h.main, id)
	delete(h.desktopReady, id)
	delete(h.nativeReady, id)
	delete(h.surfaceReady, id)
	delete(h.pixelRatios, id)
	delete(h.showing, id)
	delete(h.applied, id)
	delete(h.geometryEdits, id)
	delete(h.desktopParents, id)
	delete(h.mountErrors, id)
	key := id
	if len(id) > 9 && id[:9] == "settings-" {
		key = id[9:]
	}
	if id == "pin-chooser" {
		key = "pin"
	}
	if w == nil {
		w = h.settings[key]
		delete(h.settings, key)
	}
	if timer := h.timers[id]; timer != nil {
		timer.Stop()
		delete(h.timers, id)
	}
	h.mu.Unlock()
	if w != nil {
		application.InvokeSync(func() { h.alignment.Clear() })
		w.Close()
	}
	go h.rebuildTray()
}
func (h *Desktop) Settings(id, context string) {
	key := id
	if key == "" {
		key = "global"
	}
	h.mu.Lock()
	w := h.settings[key]
	h.mu.Unlock()
	if w != nil {
		h.focusAuxiliary("settings-"+key, w)
		return
	}
	path := "/?settings=" + url.QueryEscape(key)
	if context != "" {
		path += "&context=" + url.QueryEscape(context)
	}
	w = h.App.Window.NewWithOptions(application.WebviewWindowOptions{Name: "settings-" + key, Title: "DeskGrid 设置", URL: path, Width: 430, Height: 660, MinWidth: 380, MinHeight: 460, Hidden: true, Frameless: true, BackgroundColour: application.NewRGB(29, 30, 32), DefaultContextMenuDisabled: true})
	h.mu.Lock()
	h.settings[key] = w
	h.mu.Unlock()
	w.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		h.mu.Lock()
		delete(h.settings, key)
		delete(h.surfaceReady, "settings-"+key)
		delete(h.pixelRatios, "settings-"+key)
		h.mu.Unlock()
	})
}
func (h *Desktop) ChooseFolder() (string, error) {
	return h.App.Dialog.OpenFile().SetTitle("选择 DeskGrid 绑定目录").CanChooseDirectories(true).CanChooseFiles(false).PromptForSingleSelection()
}
func (h *Desktop) ReportError(message string) {
	if strings.Contains(strings.ToLower(message), "cancelled by user") || strings.Contains(strings.ToLower(message), "canceled by user") {
		return
	}
	slog.Warn("操作未完成", "message", message)
	h.mu.Lock()
	h.status["lastError"] = message
	h.mu.Unlock()
}
func (h *Desktop) Toggle() { go func() { h.hidePeek(); h.toggleDesktop() }() }
func (h *Desktop) toggleDesktop() {
	h.mu.Lock()
	h.hidden = !h.hidden
	hidden := h.hidden
	empty := len(h.main) == 0
	wins := map[string]*application.WebviewWindow{}
	for id, w := range h.main {
		if !h.startHiddenLocked(id) {
			wins[id] = w
		}
		if hidden {
			delete(h.showing, id)
		}
	}
	h.mu.Unlock()
	if hidden {
		application.InvokeSync(func() { h.alignment.Clear() })
	}
	if len(wins) == 0 {
		if empty {
			go h.Activate(nil)
		}
		return
	}
	g, _, _, _ := h.Service.Store.Snapshot()
	for id, w := range wins {
		if hidden {
			w.ExecJS("document.documentElement.classList.add('global-hiding')")
			win := w
			if g.ReducedMotion || shell.ReducedMotion() {
				win.Hide()
			} else {
				time.AfterFunc(180*time.Millisecond, func() {
					h.mu.Lock()
					still := h.hidden
					h.mu.Unlock()
					if still {
						win.Hide()
					}
				})
			}
		} else {
			h.showMain(id, w)
		}
	}
}
func (h *Desktop) Activate(paths []string) {
	h.mu.Lock()
	peeking := h.peekActive || h.peekTransition
	h.mu.Unlock()
	if peeking {
		go func() { h.hidePeek(); h.Activate(paths) }()
		return
	}
	h.mu.Lock()
	h.hidden = false
	wins := map[string]*application.WebviewWindow{}
	for id, w := range h.main {
		if !h.startHiddenLocked(id) {
			wins[id] = w
		}
	}
	h.mu.Unlock()
	for id, w := range wins {
		h.mount(id)
		h.showMain(id, w)
	}
	if len(paths) > 0 {
		h.Service.QueuePins(paths)
		h.pinChooser()
		return
	}
	if len(wins) == 0 {
		g, _, ws, _ := h.Service.Store.Snapshot()
		for _, w := range ws {
			if w.Type == "start" && !g.HideStartWindow {
				_, e := h.Service.Call("OpenWindow", rawMessage(map[string]any{"windowId": w.ID}))
				if e != nil {
					h.notifyError(e)
				}
				return
			}
		}
	}
}
func (h *Desktop) pinChooser() {
	h.mu.Lock()
	w := h.settings["pin"]
	h.mu.Unlock()
	if w != nil {
		h.focusAuxiliary("pin-chooser", w)
		return
	}
	w = h.App.Window.NewWithOptions(application.WebviewWindowOptions{Name: "pin-chooser", Title: "固定到 DeskGrid", URL: "/?pin=1", Width: 390, Height: 420, MinWidth: 320, MinHeight: 260, Hidden: true, Frameless: true, BackgroundColour: application.NewRGB(29, 30, 32)})
	h.mu.Lock()
	h.settings["pin"] = w
	h.mu.Unlock()
	w.OnWindowEvent(events.Common.WindowClosing, func(e *application.WindowEvent) {
		h.mu.Lock()
		delete(h.settings, "pin")
		delete(h.surfaceReady, "pin-chooser")
		delete(h.pixelRatios, "pin-chooser")
		h.mu.Unlock()
	})
}
func (h *Desktop) Quit(force bool) error {
	if h.Service.Active() {
		return errors.New("仍有文件任务正在进行，请等待完成后再退出")
	}
	h.mu.Lock()
	h.quitting = true
	h.mu.Unlock()
	go h.App.Quit()
	return nil
}
func (h *Desktop) Status() map[string]string {
	h.mu.Lock()
	defer h.mu.Unlock()
	m := map[string]string{}
	for k, v := range h.status {
		m[k] = v
	}
	return m
}
func (h *Desktop) ApplyGlobal(g, old model.Global) error {
	exe, e := os.Executable()
	if e != nil {
		return e
	}
	steps := []settingChange{}
	if g.Hotkey != old.Hotkey {
		steps = append(steps, settingChange{apply: func() error { return h.changeHotkey(g.Hotkey, old.Hotkey) }, undo: func() error { return h.changeHotkey(old.Hotkey, g.Hotkey) }})
	}
	if model.PeekBinding(g) != model.PeekBinding(old) {
		steps = append(steps, settingChange{apply: func() error { return h.changePeekHotkey(model.PeekBinding(g), model.PeekBinding(old)) }, undo: func() error { return h.changePeekHotkey(model.PeekBinding(old), model.PeekBinding(g)) }})
	}
	if g.AutoStart != old.AutoStart {
		steps = append(steps, settingChange{apply: func() error { return shell.SetAutoStart(g.AutoStart, exe) }, undo: func() error { return shell.SetAutoStart(old.AutoStart, exe) }})
	}
	if g.ShellIntegration != old.ShellIntegration {
		steps = append(steps, settingChange{apply: func() error { return shell.SetIntegration(g.ShellIntegration, exe) }, undo: func() error { return shell.SetIntegration(old.ShellIntegration, exe) }})
	}
	if e = applyChanges(steps); e != nil {
		return e
	}
	go func() {
		time.Sleep(30 * time.Millisecond)
		_, _, ws, _ := h.Service.Store.Snapshot()
		for _, w := range ws {
			h.Apply(w)
		}
	}()
	return nil
}
func (h *Desktop) changeHotkey(to, from string) error {
	if to != "" && !h.App.GlobalShortcut.IsRegistered(to) {
		if e := h.App.GlobalShortcut.Register(to, h.Toggle); e != nil {
			return errors.New("热键注册失败，请更换组合: " + e.Error())
		}
	}
	if from != "" && from != to && h.App.GlobalShortcut.IsRegistered(from) {
		if e := h.App.GlobalShortcut.Unregister(from); e != nil {
			return e
		}
	}
	h.mu.Lock()
	h.status["hotkey"] = "已清除"
	if to != "" {
		h.status["hotkey"] = "已注册 " + to
	}
	h.mu.Unlock()
	return nil
}
func (h *Desktop) Drag(id string, paths []string, virtual bool) error {
	if virtual {
		return errors.New("虚拟入口创建 .lnk 后原生拖出的异步生命周期尚未通过资源管理器验收，当前已禁用；可使用复制路径或固定到窗口，真实目标不会移动。")
	}
	var err error
	application.InvokeSync(func() { err = shell.DragFiles(paths, virtual) })
	return err
}
func (h *Desktop) StartDrag(id string) error {
	if !strings.HasPrefix(id, "settings-") && id != "pin-chooser" {
		w, e := h.Service.Store.Window(id)
		if e != nil {
			return e
		}
		if w.PositionLocked {
			return nil
		}
	}
	h.mu.Lock()
	win := h.main[id]
	if h.startHiddenLocked(id) {
		h.mu.Unlock()
		return nil
	}
	if win == nil {
		key := id
		if len(id) > 9 && id[:9] == "settings-" {
			key = id[9:]
		}
		if id == "pin-chooser" {
			key = "pin"
		}
		win = h.settings[key]
	}
	h.mu.Unlock()
	if win == nil {
		return errors.New("窗口已关闭")
	}
	application.InvokeAsync(func() { _ = native.BeginWindowDrag(uintptr(win.NativeWindow())) })
	return nil
}
func (h *Desktop) workAreas() []native.WorkArea {
	areas := []native.WorkArea{}
	for _, screen := range h.App.Screen.GetAll() {
		a := screen.WorkArea
		areas = append(areas, native.WorkArea{X: a.X, Y: a.Y, Width: a.Width, Height: a.Height})
	}
	return areas
}
func (h *Desktop) scheduleGeometry(id string) {
	h.mu.Lock()
	edit := h.geometryEdits[id]
	if edit == nil || edit.active || !edit.ready {
		h.mu.Unlock()
		return // Display changes, startup, remounts and animations are not user edits.
	}
	if old := h.timers[id]; old != nil {
		old.Stop()
	}
	h.timers[id] = time.AfterFunc(300*time.Millisecond, func() { h.saveGeometry(id) })
	h.mu.Unlock()
}
func (h *Desktop) saveGeometry(id string) {
	h.Service.mu.Lock()
	defer h.Service.mu.Unlock()
	h.mu.Lock()
	edit := h.geometryEdits[id]
	valid := edit != nil && edit.ready && !edit.active && h.main[id] != nil && !h.peekTransition && h.applied[id].sizeAnimation == nil
	var change geometryEdit
	if valid {
		change = *edit
	}
	h.mu.Unlock()
	if !valid {
		return
	}
	w, e := h.Service.Store.Window(id)
	if e != nil {
		return
	}
	b := geometryBounds(w, change)
	if b.Width < 220 || b.Height < 32 || (!w.Collapsed && b.Height < 120) {
		return
	}
	if w.Bounds != b {
		w.Bounds = b
		if !w.Collapsed {
			w.ExpandedSize = model.Size{Width: b.Width, Height: b.Height}
		}
		if e = h.Service.Store.SaveWindow(w); e != nil {
			h.notifyError(e)
			return
		}
	}
	h.mu.Lock()
	if h.geometryEdits[id] == edit && !edit.active && edit.after == change.after {
		delete(h.geometryEdits, id)
	}
	h.mu.Unlock()
}
func (h *Desktop) correctAll() {
	h.mu.Lock()
	wins := map[string]*application.WebviewWindow{}
	if h.peekActive || h.peekTransition || h.quitting {
		h.mu.Unlock()
		return
	}
	for id, w := range h.main {
		if edit := h.geometryEdits[id]; edit != nil && edit.active {
			continue
		}
		if h.applied[id].sizeAnimation != nil {
			continue
		}
		wins[id] = w
	}
	h.mu.Unlock()
	areas := h.workAreas()
	for id, w := range wins {
		if !native.IsDesktopAttached(uintptr(w.NativeWindow())) {
			continue
		}
		b := w.Bounds()
		saved, err := h.Service.Store.Window(id)
		if err != nil {
			continue
		}
		preferred := h.preferredBounds(saved)
		x, y, width, height := native.Clamp(int(preferred.X), int(preferred.Y), int(preferred.Width), int(preferred.Height), areas)
		if x != b.X || y != b.Y || width != b.Width || height != b.Height {
			h.setMainBounds(w, x, y, width, height)
		}
		h.clipWindow(id, w)
	}
}
func (h *Desktop) maintainDesktop() {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.mu.Lock()
			if h.peekActive || h.peekTransition {
				h.mu.Unlock()
				continue
			}
			ids := []string{}
			for id := range h.main {
				ids = append(ids, id)
			}
			h.mu.Unlock()
			for _, id := range ids {
				h.mu.Lock()
				w := h.main[id]
				ready := h.nativeReady[id]
				h.mu.Unlock()
				if w == nil || !ready {
					continue
				}
				hwnd := uintptr(w.NativeWindow())
				if !native.IsNativeWindow(hwnd) {
					go h.restoreDesktopWindow(id)
					continue
				}
				if !native.IsDesktopAttached(hwnd) {
					h.mount(id)
				}
			}
		}
	}
}
func (h *Desktop) flush() {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
	h.Service.stopWatches()
	h.mu.Lock()
	h.quitting = true
	input := h.peekInput
	if h.peekFocusTimer != nil {
		h.peekFocusTimer.Stop()
	}
	ids := []string{}
	for id, t := range h.timers {
		t.Stop()
		ids = append(ids, id)
	}
	h.mu.Unlock()
	application.InvokeSync(func() { h.alignment.Clear() })
	if input != nil {
		input.Close()
	}
	for _, id := range ids {
		h.saveGeometry(id)
	}
}
func (h *Desktop) notifyError(e error) {
	slog.Error("DeskGrid", "error", e)
	h.ReportError(e.Error())
}
func (h *Desktop) rebuildTray() {
	if h.tray == nil {
		return
	}
	menu := h.App.NewMenu()
	menu.Add("显示 / 隐藏全部").OnClick(func(c *application.Context) { h.Toggle() })
	menu.Add("创建应用窗口").OnClick(func(c *application.Context) {
		_, e := h.Service.Call("CreateWindow", rawMessage(map[string]any{"type": "collection"}))
		if e != nil {
			h.notifyError(e)
		}
	})
	menu.Add("打开文件夹窗口…").OnClick(func(c *application.Context) {
		path, e := h.ChooseFolder()
		if e == nil && path != "" {
			_, e = h.Service.Call("CreateWindow", rawMessage(map[string]any{"type": "folder", "path": path}))
		}
		if e != nil {
			h.notifyError(e)
		}
	})
	menu.AddSeparator()
	menu.Add("设置…").OnClick(func(c *application.Context) { h.Settings("", "") })
	menu.Add("退出 DeskGrid").OnClick(func(c *application.Context) {
		if e := h.Quit(false); e != nil {
			h.notifyError(e)
		}
	})
	h.tray.SetMenu(menu)
}
func rawMessage(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
