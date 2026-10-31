package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"deskgrid/internal/appearance"
	"deskgrid/internal/commands"
	"deskgrid/internal/config"
	"deskgrid/internal/filesystem"
	"deskgrid/internal/model"
	"deskgrid/internal/shell"
)

// Host is the narrow native-window boundary; configuration and real filesystem work stay in Go.
type Host interface {
	Open(model.Window) error
	Close(string)
	Apply(model.Window)
	Settings(string, string)
	ChooseFolder() (string, error)
	Toggle()
	Quit(bool) error
	ApplyGlobal(model.Global, model.Global) error
	Status() map[string]string
	Drag(string, []string, bool) error
	StartDrag(string) error
}
type Service struct {
	Store        *config.Store
	Files        *filesystem.Manager
	Host         Host
	mu           sync.Mutex
	pending      []string
	recycleMu    sync.Mutex
	recycle      map[string]*recycleTask
	watchMu      sync.Mutex
	watchers     map[string]map[string]func()
	changeTimers map[string]*time.Timer
	Notify       func(string)
}
type recycleTask struct {
	Task filesystem.Task
}

func NewService(store *config.Store, host Host) *Service {
	return &Service{Store: store, Files: filesystem.NewManager(), Host: host, pending: []string{}, recycle: map[string]*recycleTask{}, watchers: map[string]map[string]func(){}, changeTimers: map[string]*time.Timer{}}
}

type params struct {
	WindowID       string             `json:"windowId"`
	SourceWindowID string             `json:"sourceWindowId"`
	TargetWindowID string             `json:"targetWindowId"`
	TargetEntryID  string             `json:"targetEntryId"`
	RequestID      string             `json:"requestId"`
	Type           string             `json:"type"`
	Path           string             `json:"path"`
	Title          string             `json:"title"`
	Name           string             `json:"name"`
	Root           string             `json:"root"`
	Paths          []string           `json:"paths"`
	IDs            []string           `json:"ids"`
	GroupID        string             `json:"groupId"`
	Index          int                `json:"index"`
	InsertIndex    *int               `json:"insertIndex"`
	Operation      string             `json:"operation"`
	Destination    string             `json:"destination"`
	Conflict       string             `json:"conflict"`
	Copy           bool               `json:"copy"`
	Cut            bool               `json:"cut"`
	Directory      bool               `json:"directory"`
	Force          bool               `json:"force"`
	Virtual        bool               `json:"virtual"`
	Verb           string             `json:"verb"`
	Text           string             `json:"text"`
	Offset         int                `json:"offset"`
	Limit          int                `json:"limit"`
	Patch          json.RawMessage    `json:"patch"`
	Context        string             `json:"context"`
	IsGlobal       bool               `json:"global"`
	Template       string             `json:"template"`
	Mode           string             `json:"mode"`
	ActionID       string             `json:"actionId"`
	PixelRatio     float64            `json:"pixelRatio"`
	X              float64            `json:"x"`
	Y              float64            `json:"y"`
	Actions        []model.MenuAction `json:"actions"`
}

func (s *Service) Call(method string, data json.RawMessage) (any, error) {
	var p params
	if len(data) > 0 && string(data) != "null" {
		if e := json.Unmarshal(data, &p); e != nil {
			return nil, fmt.Errorf("请求参数无效: %w", e)
		}
	}
	// Slow I/O and native modal loops must not hold the mutation lock: cancellation
	// and interactions in another native window remain available while they run.
	switch method {
	case "BeginPeekRecording":
		if host, ok := s.Host.(interface{ BeginPeekRecording(string, string) error }); ok {
			return nil, host.BeginPeekRecording(p.WindowID, p.RequestID)
		}
		return nil, errors.New("快捷键录制不可用")
	case "EndPeekRecording":
		if host, ok := s.Host.(interface{ EndPeekRecording(string, string) }); ok {
			host.EndPeekRecording(p.WindowID, p.RequestID)
		}
		return nil, nil
	case "TogglePeek":
		if host, ok := s.Host.(interface{ TogglePeek() }); ok {
			host.TogglePeek()
		}
		return nil, nil
	case "ShowContextMenu":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return "", e
		}
		if w.Collapsed {
			return "", nil
		}
		if len(p.Actions) > 100 {
			return "", errors.New("菜单项目过多")
		}
		if host, ok := s.Host.(interface {
			ContextMenu(string, []model.MenuAction, float64, float64) (string, error)
		}); ok {
			return host.ContextMenu(p.WindowID, p.Actions, p.X, p.Y)
		}
		return "", nil
	case "UndoTransfer":
		return s.Files.UndoLast()
	case "WindowInteracted":
		if host, ok := s.Host.(interface{ Interact(string) }); ok {
			host.Interact(p.WindowID)
		}
		return nil, nil
	case "WindowPainted":
		if p.PixelRatio < .25 || p.PixelRatio > 8 {
			return nil, errors.New("窗口像素比例无效")
		}
		if host, ok := s.Host.(interface{ SurfaceReady(string, float64) }); ok {
			host.SurfaceReady(p.WindowID, p.PixelRatio)
		}
		return nil, nil
	case "Navigate":
		return s.navigate(p)
	case "CheckTransferConflicts":
		return s.checkTransferConflicts(p)
	case "GetTransferTasks":
		return s.Files.States(p.IDs), nil
	case "TransferFiles":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		if w.Type != "folder" {
			return nil, errors.New("真实文件目标必须是单文件夹窗口")
		}
		if e = filesystem.Confined(w.RootPath, p.Destination); e != nil {
			return nil, e
		}
		if p.Conflict == "" || p.Conflict == "ask" {
			p.Conflict = "keepBoth"
		}
		return s.Files.Start(p.Paths, p.Destination, p.Operation, p.Conflict)
	case "ListDirectory":
		return s.listDirectory(p)
	case "StatPaths":
		return filesystem.Stat(p.Paths), nil
	case "OpenPath":
		return nil, shell.Open(p.Path, p.Verb)
	case "ChooseFolder":
		if s.Host == nil {
			return "", errors.New("本机窗口不可用")
		}
		return s.Host.ChooseFolder()
	case "BeginDrag":
		if s.Host == nil {
			return nil, errors.New("原生拖出不可用")
		}
		return nil, s.Host.Drag(p.WindowID, p.Paths, p.Virtual)
	case "NativeDragStart":
		if s.Host != nil {
			return nil, s.Host.StartDrag(p.WindowID)
		}
		return nil, nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	switch method {
	case "Snapshot":
		return s.snapshot(p.WindowID)
	case "CreateWindow":
		return s.create(p.Type, p.Title, p.Path)
	case "OpenWindow":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		if s.Host != nil {
			if e = s.Host.Open(w); e != nil {
				return nil, e
			}
		}
		return nil, s.setOpen(w.ID, true)
	case "CloseWindow":
		if strings.HasPrefix(p.WindowID, "settings-") || p.WindowID == "pin-chooser" {
			if s.Host != nil {
				s.Host.Close(p.WindowID)
			}
			return nil, nil
		}
		return nil, s.removeWindow(p.WindowID)
	case "DeleteWindow":
		return nil, s.removeWindow(p.WindowID)
	case "UpdateWindow":
		return s.update(p.WindowID, p.Patch)
	case "Pin":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		if p.TargetEntryID != "" && p.GroupID != "" {
			return nil, errors.New("不能同时指定应用文件夹和成组入口")
		}
		previousIDs := entryIDs(w.Items)
		for _, path := range p.Paths {
			if _, e = os.Lstat(path); e != nil {
				return nil, fmt.Errorf("无法固定 %s: %w", path, e)
			}
		}
		duplicates, e := shortPin(&w, p.Paths, p.GroupID)
		if e != nil {
			return nil, e
		}
		if p.TargetEntryID != "" {
			if e = groupDroppedEntries(&w, newEntryIDs(w.Items, previousIDs), p.TargetEntryID); e != nil {
				return nil, e
			}
		} else if p.GroupID == "" && p.InsertIndex != nil {
			ids := newEntryIDs(w.Items, previousIDs)
			if len(ids) > 0 {
				if e = mutate(&w, params{Operation: "reorder", IDs: ids, Index: *p.InsertIndex}); e != nil {
					return nil, e
				}
			}
		}
		saved, e := s.save(w)
		return map[string]any{"window": saved, "duplicates": duplicates}, e
	case "MutateEntries":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		if e = mutate(&w, p); e != nil {
			return nil, e
		}
		return s.save(w)
	case "TransferEntries":
		a, e := s.Store.Window(p.SourceWindowID)
		if e != nil {
			return nil, e
		}
		b, e := s.Store.Window(p.TargetWindowID)
		if e != nil {
			return nil, e
		}
		if a.ID == b.ID {
			return nil, errors.New("同窗口请使用排序或分组操作")
		}
		if e = transfer(&a, &b, p); e != nil {
			return nil, e
		}
		// Save the destination first: a crash can leave duplicate entries, never lose the only entry.
		if _, e = s.save(b); e != nil {
			return nil, e
		}
		if _, e = s.save(a); e != nil {
			return nil, fmt.Errorf("目标已保存，来源未更新（保留入口）: %w", e)
		}
		return nil, nil
	case "ClipboardWrite":
		return nil, shell.ClipboardWrite(p.Paths, p.Cut)
	case "ClipboardText":
		return nil, shell.ClipboardText(p.Text)
	case "Paste":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		paths, cut, e := shell.ClipboardFiles()
		if e != nil {
			return nil, e
		}
		if w.Type != "folder" {
			_, e = shortPin(&w, paths, "")
			if e != nil {
				return nil, e
			}
			return s.save(w)
		}
		dest := p.Path
		if dest == "" {
			dest = w.CurrentPath
		}
		if e = filesystem.Confined(w.RootPath, dest); e != nil {
			return nil, e
		}
		op := "copy"
		if cut {
			op = "move"
		}
		if p.Conflict == "" || p.Conflict == "ask" {
			p.Conflict = "keepBoth"
		}
		return s.Files.Start(paths, dest, op, p.Conflict)
	case "Recycle":
		return s.startRecycle(p.Paths)
	case "RenameFile", "CreateFile":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		if w.Type != "folder" {
			return nil, errors.New("只有真实目录窗口可以修改磁盘文件")
		}
		if method == "RenameFile" {
			e = filesystem.Rename(w.RootPath, p.Path, p.Name)
		} else {
			e = filesystem.Create(w.RootPath, p.Path, p.Name, p.Directory)
		}
		return nil, e
	case "SaveGlobal":
		g, _, _, _ := s.Store.Snapshot()
		g.DesktopMode = true
		old := model.Clone(g)
		if e := json.Unmarshal(p.Patch, &g); e != nil {
			return nil, e
		}
		if g.SchemaVersion != 1 {
			return nil, errors.New("不能更改配置版本")
		}
		if !g.DesktopMode {
			return nil, errors.New("主内容窗口只能显示在桌面，不能切换为普通应用窗口")
		}
		if e := appearance.ValidatePatch(g.Appearance); e != nil {
			return nil, e
		}
		visibilityMods, visibilityKey, e := shell.ParseHotkey(g.Hotkey)
		if e != nil {
			return nil, e
		}
		peek := strings.TrimSpace(model.PeekBinding(g))
		peekMods, peekKey, e := shell.ParseHotkey(peek)
		if e != nil {
			return nil, e
		}
		if peekKey != 0 && peekKey == visibilityKey && peekMods == visibilityMods {
			return nil, errors.New("临时前置与桌面显示/隐藏不能使用同一个快捷键")
		}
		if peekKey != 0 {
			peek, _ = shell.FormatHotkey(peekMods, peekKey)
		}
		g.PeekHotkey = &peek
		if s.Host != nil {
			if e := s.Host.ApplyGlobal(g, old); e != nil {
				return nil, e
			}
		}
		if e := s.Store.SaveGlobal(g); e != nil {
			if s.Host != nil {
				if rollback := s.Host.ApplyGlobal(old, g); rollback != nil {
					return nil, errors.Join(e, fmt.Errorf("系统设置恢复失败，请检查全局状态: %w", rollback))
				}
			}
			return nil, e
		}
		if g.HideStartWindow != old.HideStartWindow {
			if host, ok := s.Host.(interface{ RefreshStartVisibility() }); ok {
				host.RefreshStartVisibility()
			}
		}
		return g, nil
	case "ApplyAppearanceGlobal":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		g, _, _, _ := s.Store.Snapshot()
		a := appearance.Resolve(g, w, shell.SystemDark())
		b, _ := json.Marshal(a)
		_ = json.Unmarshal(b, &g.Appearance)
		return nil, s.Store.SaveGlobal(g)
	case "ResetAppearance":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		w.AppearanceOverrides = model.AppearancePatch{}
		return s.save(w)
	case "GetMenu":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		g, _, _, _ := s.Store.Snapshot()
		if p.IsGlobal {
			w.MenuOverrides = map[string]model.MenuPatch{}
		}
		return commands.ResolveMenu(g, w, p.Context), nil
	case "SaveMenu", "ResetMenu":
		return nil, s.saveMenu(p, method == "ResetMenu")
	case "PreviewCommand":
		return commands.Parse(p.Template, p.Mode, p.Paths, p.Root)
	case "RunCommand":
		return nil, s.runCommand(p)
	case "Settings":
		if s.Host != nil {
			s.Host.Settings(p.WindowID, p.Context)
		}
		return nil, nil
	case "ToggleAll":
		if s.Host != nil {
			s.Host.Toggle()
		}
		return nil, nil
	case "ReportError":
		if host, ok := s.Host.(interface{ ReportError(string) }); ok && p.Text != "" {
			host.ReportError(p.Text)
		}
		return nil, nil
	case "Quit":
		if s.Host != nil {
			return nil, s.Host.Quit(p.Force)
		}
		return nil, nil
	case "FinishPin":
		w, e := s.Store.Window(p.WindowID)
		if e != nil {
			return nil, e
		}
		if w.Type != "collection" {
			return nil, errors.New("请选择应用窗口")
		}
		if _, e = shortPin(&w, s.pending, ""); e != nil {
			return nil, e
		}
		if _, e = s.save(w); e != nil {
			return nil, e
		}
		s.pending = []string{}
		return nil, nil
	default:
		return nil, fmt.Errorf("不支持的操作 %q", method)
	}
}
func (s *Service) listDirectory(p params) (any, error) {
	w, e := s.Store.Window(p.WindowID)
	if e != nil {
		return nil, e
	}
	if w.Type == "folder" {
		if e = filesystem.Confined(w.RootPath, p.Path); e != nil {
			return nil, e
		}
	}
	if w.Type == "collection" {
		return nil, errors.New("应用窗口不扫描固定目录")
	}
	g, _, _, _ := s.Store.Snapshot()
	listing, e := filesystem.List(p.Path, g.HiddenFiles, p.Offset, p.Limit)
	if e == nil {
		s.watch(w.ID, p.Path)
	}
	return listing, e
}

func (s *Service) navigate(p params) (model.Window, error) {
	w, e := s.Store.Window(p.WindowID)
	if e != nil {
		return w, e
	}
	if w.Type != "folder" {
		return w, errors.New("只有单文件夹窗口可以导航")
	}
	if e = filesystem.Confined(w.RootPath, p.Path); e != nil {
		return w, e
	}
	st, e := os.Stat(p.Path)
	if e != nil || !st.IsDir() {
		return w, fmt.Errorf("目录不可访问: %s", p.Path)
	}
	f, e := os.Open(p.Path)
	if e != nil {
		return w, e
	}
	f.Close()
	s.mu.Lock()
	defer s.mu.Unlock()
	w, e = s.Store.Window(p.WindowID)
	if e != nil {
		return w, e
	}
	w.CurrentPath = filepath.Clean(p.Path)
	if w.TitleMode != "custom" {
		w.Title = folderTitle(w.CurrentPath)
	}
	if _, e = s.save(w); e != nil {
		return w, e
	}
	if e = s.reconcileFolderTitles(); e != nil {
		return w, e
	}
	return s.Store.Window(w.ID)
}
func (s *Service) save(w model.Window) (model.Window, error) {
	if e := s.Store.SaveWindow(w); e != nil {
		return w, e
	}
	w, e := s.Store.Window(w.ID)
	keep := append([]string{}, w.ExpandedPaths...)
	if w.Type == "folder" {
		keep = append(keep, w.CurrentPath)
	}
	s.pruneWatches(w.ID, keep)
	if e == nil && s.Host != nil {
		s.Host.Apply(w)
	}
	return w, e
}
func (s *Service) removeWindow(id string) error {
	w, e := s.Store.Window(id)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	if w.Type == "start" || w.PositionLocked {
		return nil
	}
	if e = s.Store.DeleteWindow(id); e != nil {
		return e
	}
	s.pruneWatches(id, nil)
	if s.Host != nil {
		s.Host.Close(id)
	}
	return s.reconcileFolderTitles()
}

func (s *Service) setOpen(id string, opened bool) error {
	_, session, _, _ := s.Store.Snapshot()
	out := []string{}
	found := false
	for _, v := range session.OpenWindowIDs {
		if v == id {
			found = true
			if !opened {
				continue
			}
		}
		out = append(out, v)
	}
	if opened && !found {
		out = append(out, id)
	}
	session.OpenWindowIDs = out
	if e := s.Store.SaveSession(session); e != nil {
		return e
	}
	return s.reconcileFolderTitles()
}

// Only currently open windows reserve display titles. Closed definitions keep
// their identity and are assigned an available title when opened again.
func (s *Service) availableTitle(base string) string {
	_, session, all, _ := s.Store.Snapshot()
	opened := map[string]bool{}
	for _, id := range session.OpenWindowIDs {
		opened[id] = true
	}
	used := map[string]bool{}
	for _, w := range all {
		if opened[w.ID] {
			used[strings.ToLower(w.Title)] = true
		}
	}
	title := base
	for n := 2; used[strings.ToLower(title)]; n++ {
		title = fmt.Sprintf("%s - %d", base, n)
	}
	return title
}

func (s *Service) reconcileFolderTitles() error {
	_, session, all, _ := s.Store.Snapshot()
	byID := map[string]model.Window{}
	for _, w := range all {
		byID[w.ID] = w
	}
	used := map[string]bool{}
	for _, id := range session.OpenWindowIDs {
		w := byID[id]
		if w.Type != "folder" || w.TitleMode == "custom" {
			used[strings.ToLower(w.Title)] = true
		}
	}
	for _, id := range session.OpenWindowIDs {
		w, ok := byID[id]
		if !ok || w.Type != "folder" || w.TitleMode == "custom" {
			continue
		}
		base := folderTitle(w.CurrentPath)
		title := base
		for n := 2; used[strings.ToLower(title)]; n++ {
			title = fmt.Sprintf("%s - %d", base, n)
		}
		used[strings.ToLower(title)] = true
		if title != w.Title {
			w.Title = title
			if e := s.Store.SaveWindow(w); e != nil {
				return e
			}
			if s.Host != nil {
				s.Host.Apply(w)
			}
		}
	}
	return nil
}
func (s *Service) create(kind, title, path string) (model.Window, error) {
	if kind != "collection" && kind != "folder" {
		return model.Window{}, errors.New("只支持创建应用窗口或单文件夹窗口")
	}
	if kind == "folder" {
		if !filepath.IsAbs(path) {
			return model.Window{}, errors.New("需要绝对目录路径")
		}
		st, e := os.Stat(path)
		if e != nil || !st.IsDir() {
			return model.Window{}, fmt.Errorf("目录不可访问: %s", path)
		}
		path = filepath.Clean(path)
		if title == "" {
			title = folderTitle(path)
		}
	} else if title == "" {
		title = "应用窗口"
	}
	_, _, all, _ := s.Store.Snapshot()
	title = s.availableTitle(title)
	w := model.DefaultWindow(kind, title, path)
	if kind == "collection" {
		w.TitleMode = "custom"
	}
	w.Bounds.X += float64(len(all)%9) * 24
	w.Bounds.Y += float64(len(all)%9) * 24
	v, e := s.save(w)
	if e != nil {
		return v, e
	}
	if s.Host != nil {
		if e = s.Host.Open(v); e != nil {
			return v, e
		}
	}
	if e = s.setOpen(v.ID, true); e != nil {
		return v, e
	}
	return s.Store.Window(v.ID)
}
func folderTitle(path string) string {
	if filepath.Clean(path) == filepath.VolumeName(path)+`\` {
		for _, d := range shell.Drives() {
			if strings.EqualFold(d.Path, path) {
				return d.Name
			}
		}
	}
	return filepath.Base(path)
}
func (s *Service) update(id string, patch json.RawMessage) (model.Window, error) {
	w, e := s.Store.Window(id)
	if e != nil {
		return w, e
	}
	previousCollapsed := w.Collapsed
	previousBounds := w.Bounds
	var fields map[string]json.RawMessage
	if e = json.Unmarshal(patch, &fields); e != nil {
		return w, e
	}
	for _, k := range []string{"id", "type", "schemaVersion", "revision", "rootPath", "currentPath"} {
		if _, ok := fields[k]; ok {
			return w, fmt.Errorf("字段 %s 不能通过窗口更新更改", k)
		}
	}
	if e = json.Unmarshal(patch, &w); e != nil {
		return w, e
	}
	if w.PositionLocked && w.Collapsed != previousCollapsed {
		return w, errors.New("窗口已固定，请先取消固定再展开或收缩")
	}
	if w.CollapsedHeight != 0 && (w.CollapsedHeight < 24 || w.CollapsedHeight > 96) {
		return w, errors.New("标题分界线高度无效")
	}
	if w.Collapsed != previousCollapsed {
		if w.Collapsed {
			bounds := previousBounds
			if host, ok := s.Host.(interface {
				CurrentBounds(string) (model.Bounds, bool)
			}); ok {
				if current, valid := host.CurrentBounds(id); valid && current.Height >= 120 {
					bounds = current
				}
			}
			if bounds.Width >= 220 && bounds.Height >= 120 {
				w.ExpandedSize = model.Size{Width: bounds.Width, Height: bounds.Height}
				w.Bounds.X = bounds.X
				w.Bounds.Y = bounds.Y
				w.Bounds.Width = bounds.Width
			}
			w.Bounds.Height = float64(model.CollapsedWindowHeight(w))
		} else {
			// Restore the saved dimensions before config validation, not after saving.
			w.Bounds.Width = max(220, w.ExpandedSize.Width)
			w.Bounds.Height = max(120, w.ExpandedSize.Height)
		}
	}
	if w.Collapsed {
		w.Bounds.Height = float64(model.CollapsedWindowHeight(w))
	}
	if b, ok := fields["appearanceOverrides"]; ok {
		var replacement model.AppearancePatch
		if e = json.Unmarshal(b, &replacement); e != nil {
			return w, e
		}
		w.AppearanceOverrides = replacement
	}
	if w.Title == "" || len([]rune(w.Title)) > 120 {
		return w, errors.New("标题须为 1–120 个字符")
	}
	if w.Type == "folder" {
		w.Layout = "list"
	}
	if e = appearance.ValidatePatch(w.AppearanceOverrides); e != nil {
		return w, e
	}
	if w.Bounds.Width < 220 || w.Bounds.Height < 32 {
		return w, errors.New("窗口尺寸过小")
	}
	return s.save(w)
}
func (s *Service) snapshot(id string) (any, error) {
	g, session, ws, warnings := s.Store.Snapshot()
	g.DesktopMode = true
	var current *model.Window
	for _, w := range ws {
		if w.ID == id {
			copy := w
			current = &copy
			break
		}
	}
	a := appearance.Resolve(g, model.Window{}, shell.SystemDark())
	if current != nil {
		a = appearance.Resolve(g, *current, shell.SystemDark())
	}
	status := map[string]string{"desktop": "仅在桌面显示", "hotkey": "未注册"}
	if s.Host != nil {
		status = s.Host.Status()
	}
	auto, integration := shell.RegistrationStatus()
	status["autoStart"] = auto
	status["shellIntegration"] = integration
	if warnings == nil {
		warnings = []string{}
	}
	return map[string]any{"window": current, "windows": ws, "openWindowIds": session.OpenWindowIDs, "global": g, "appearance": a, "drives": shell.Drives(), "warnings": warnings, "status": status, "pendingPins": append([]string{}, s.pending...), "systemDark": shell.SystemDark()}, nil
}
func (s *Service) saveMenu(p params, reset bool) error {
	if p.Context != "blank" && p.Context != "folder" && p.Context != "file" && p.Context != "group" {
		return errors.New("菜单上下文无效")
	}
	w, e := s.Store.Window(p.WindowID)
	if e != nil {
		return e
	}
	var patch model.MenuPatch
	if !reset {
		if e = json.Unmarshal(p.Patch, &patch); e != nil {
			return e
		}
		for _, a := range patch.Actions {
			if a.Template != nil && !(a.Enabled != nil && !*a.Enabled && *a.Template == "") {
				mode := "program"
				if a.Mode != nil {
					mode = *a.Mode
				}
				paths := []string{`C:\DeskGrid Example\file.txt`}
				root := w.RootPath
				parsed, err := commands.Parse(*a.Template, mode, paths, root)
				if err != nil {
					return err
				}
				for _, c := range parsed {
					program, err := exec.LookPath(c.Program)
					if err != nil {
						return fmt.Errorf("找不到程序 %s，请填写真实可执行文件路径", c.Program)
					}
					if mode == "program" && (strings.EqualFold(filepath.Ext(program), ".cmd") || strings.EqualFold(filepath.Ext(program), ".bat")) {
						return errors.New("程序解析为批处理文件，请选择 cmd / PowerShell 模式或真实 exe")
					}
				}
			}
		}
	}
	if p.IsGlobal {
		g, _, _, _ := s.Store.Snapshot()
		key := w.Type + ":" + p.Context
		if reset {
			delete(g.Menus, key)
		} else {
			g.Menus[key] = mergeMenu(g.Menus[key], patch)
		}
		return s.Store.SaveGlobal(g)
	}
	if reset {
		delete(w.MenuOverrides, p.Context)
	} else {
		w.MenuOverrides[p.Context] = mergeMenu(w.MenuOverrides[p.Context], patch)
	}
	_, e = s.save(w)
	return e
}
func mergeMenu(base, patch model.MenuPatch) model.MenuPatch {
	if patch.Order != nil {
		base.Order = patch.Order
	}
	if base.Actions == nil {
		base.Actions = map[string]model.MenuActionPatch{}
	}
	for id, a := range patch.Actions {
		old := base.Actions[id]
		if a.Name != nil {
			old.Name = a.Name
		}
		if a.Enabled != nil {
			old.Enabled = a.Enabled
		}
		if a.Custom != nil {
			old.Custom = a.Custom
		}
		if a.Mode != nil {
			old.Mode = a.Mode
		}
		if a.Template != nil {
			old.Template = a.Template
		}
		base.Actions[id] = old
	}
	return base
}
func (s *Service) runCommand(p params) error {
	w, e := s.Store.Window(p.WindowID)
	if e != nil {
		return e
	}
	g, _, _, _ := s.Store.Snapshot()
	for _, action := range commands.ResolveMenu(g, w, p.Context) {
		if action.ID != p.ActionID {
			continue
		}
		if !action.Custom || !action.Enabled {
			return errors.New("该动作不是已启用的自定义命令")
		}
		mode := action.Mode
		if mode == "" {
			mode = "program"
		}
		cmds, e := commands.Parse(action.Template, mode, p.Paths, w.RootPath)
		if e != nil {
			return e
		}
		for _, c := range cmds {
			program, e := exec.LookPath(c.Program)
			if e != nil {
				return fmt.Errorf("找不到程序 %s: %w", c.Program, e)
			}
			ext := strings.ToLower(filepath.Ext(program))
			if mode == "program" && (ext == ".cmd" || ext == ".bat") {
				return errors.New("程序解析为 cmd/bat，请改用明确的 Shell 模式或选择真正的 exe")
			}
			command, e := newCustomProcess(program, c.Args, c.Directory, mode)
			if e != nil {
				return e
			}
			output := &commandOutput{}
			command.Stdout, command.Stderr = output, output
			if e = command.Start(); e != nil {
				return fmt.Errorf("启动命令 %s 失败: %w", action.Name, e)
			}
			go func(name, windowID string) {
				if err := command.Wait(); err != nil && !errors.Is(err, exec.ErrWaitDelay) {
					message := fmt.Sprintf("命令 %s 执行失败: %v", name, err)
					if detail := output.String(); detail != "" {
						message += "\n" + detail
					}
					slog.Warn("自定义命令执行失败", "windowId", windowID, "program", program, "directory", c.Directory, "error", message)
					if host, ok := s.Host.(interface{ ReportError(string) }); ok {
						host.ReportError(message)
					}
				}
			}(action.Name, w.ID)
		}
		return nil
	}
	return errors.New("菜单命令不存在")
}
func (s *Service) startRecycle(paths []string) (filesystem.Task, error) {
	if len(paths) == 0 {
		return filesystem.Task{}, errors.New("未选择文件")
	}
	for _, p := range paths {
		if !filepath.IsAbs(p) || filepath.Clean(p) == filepath.VolumeName(p)+`\` {
			return filesystem.Task{}, errors.New("不能删除磁盘或非绝对路径")
		}
	}
	task := filesystem.Task{ID: model.NewID(), Operation: "recycle", Status: "running", Total: len(paths), Results: []filesystem.Result{}}
	r := &recycleTask{Task: task}
	s.recycleMu.Lock()
	s.recycle[task.ID] = r
	s.recycleMu.Unlock()
	go func() {
		failed := false
		for _, p := range paths {
			e := shell.RecycleOne(p)
			result := filesystem.Result{Source: p, Status: "completed"}
			if e != nil {
				result.Status = "failed"
				result.Error = e.Error()
				failed = true
			}
			s.recycleMu.Lock()
			r.Task.Results = append(r.Task.Results, result)
			r.Task.Completed++
			s.recycleMu.Unlock()
		}
		s.recycleMu.Lock()
		r.Task.Status = "completed"
		if failed {
			r.Task.Status = "failed"
		}
		s.recycleMu.Unlock()
	}()
	return task, nil
}
func (s *Service) Active() bool {
	if s.Files.Active() {
		return true
	}
	s.recycleMu.Lock()
	defer s.recycleMu.Unlock()
	for _, r := range s.recycle {
		if r.Task.Status == "running" {
			return true
		}
	}
	return false
}
func (s *Service) QueuePins(paths []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = append(s.pending, paths...)
}
