package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"deskgrid/internal/appearance"
	"deskgrid/internal/model"
	"strings"
	"sync"
	"time"
)

type Store struct {
	mu        sync.Mutex
	dir       string
	global    model.Global
	session   model.Session
	windows   map[string]model.Window
	files     map[string]string
	warnings  []string
	protected map[string]bool
}

var uuid = regexp.MustCompile(`^[a-fA-F0-9]{8}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{4}-[a-fA-F0-9]{12}$`)
var invalid = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

func cleanTitle(s string) string {
	s = strings.Trim(invalid.ReplaceAllString(s, "_"), " .")
	r := []rune(s)
	if len(r) > 60 {
		s = string(r[:60])
	}
	if s == "" {
		s = "窗口"
	}
	return s
}
func schema(b []byte) error {
	var h struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if e := json.Unmarshal(b, &h); e != nil {
		return e
	}
	if h.SchemaVersion != model.SchemaVersion {
		return fmt.Errorf("unsupported schemaVersion %d", h.SchemaVersion)
	}
	return nil
}
func (s *Store) read(path string, dst any) error {
	b, e := os.ReadFile(path)
	if e != nil {
		return e
	}
	if e = schema(b); e == nil {
		e = decode(b, dst)
	}
	if e == nil {
		return nil
	}
	var head struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	_ = json.Unmarshal(b, &head)
	if head.SchemaVersion > model.SchemaVersion {
		s.protected[path] = true
		var identity struct {
			ID string `json:"id"`
		}
		_ = json.Unmarshal(b, &identity)
		if uuid.MatchString(identity.ID) {
			s.protected[identity.ID] = true
		}
		return e
	}
	if ie := atomicWrite(filepath.Join(s.dir, "backups", filepath.Base(path)+fmt.Sprintf(".corrupt-%d", time.Now().UnixNano())), b); ie != nil {
		s.warnings = append(s.warnings, "could not isolate corrupt file: "+ie.Error())
	}
	backup := filepath.Join(s.dir, "backups", filepath.Base(path)+".bak")
	bb, be := os.ReadFile(backup)
	if be == nil {
		be = schema(bb)
	}
	if be == nil {
		be = decode(bb, dst)
	}
	s.warnings = append(s.warnings, filepath.Base(path)+": "+e.Error())
	if be == nil {
		s.warnings = append(s.warnings, "recovered "+filepath.Base(path)+" from backup")
		return nil
	}
	return e
}
func Open(directory string) (*Store, error) {
	s := &Store{dir: directory, global: model.DefaultGlobal(), session: model.Session{SchemaVersion: 1, OpenWindowIDs: []string{}}, windows: map[string]model.Window{}, files: map[string]string{}, protected: map[string]bool{}, warnings: []string{}}
	for _, p := range []string{directory, filepath.Join(directory, "windows"), filepath.Join(directory, "backups"), filepath.Join(directory, "logs")} {
		if e := os.MkdirAll(p, 0700); e != nil {
			return nil, e
		}
	}
	sessionLoaded := false
	for _, v := range []struct {
		name string
		dst  any
	}{{"global.json", &s.global}, {"session.json", &s.session}} {
		e := s.read(filepath.Join(directory, v.name), v.dst)
		if v.name == "session.json" && e == nil {
			sessionLoaded = true
		}
		if e != nil && !os.IsNotExist(e) {
			s.warnings = append(s.warnings, v.name+": "+e.Error())
		}
	}
	paths, e := filepath.Glob(filepath.Join(directory, "windows", "*.json"))
	if e != nil {
		return nil, e
	}
	for _, p := range paths {
		var w model.Window
		if e = s.read(p, &w); e != nil {
			s.warnings = append(s.warnings, filepath.Base(p)+": "+e.Error())
			continue
		}
		if e = validateWindow(w); e != nil {
			s.warnings = append(s.warnings, filepath.Base(p)+": "+e.Error())
			continue
		}
		if old, ok := s.windows[w.ID]; !ok || w.Revision > old.Revision {
			s.windows[w.ID] = w
			s.files[w.ID] = p
		}
	}
	hasStart := false
	for _, w := range s.windows {
		if w.Type == "start" {
			hasStart = true
		}
	}
	if !hasStart {
		w := model.DefaultWindow("start", "此电脑", "")
		if e = s.SaveWindow(w); e != nil {
			return nil, e
		}
		if len(s.session.OpenWindowIDs) == 0 {
			s.session.OpenWindowIDs = []string{w.ID}
		}
	}
	if _, e = os.Stat(filepath.Join(directory, "global.json")); os.IsNotExist(e) {
		if e = s.SaveGlobal(s.global); e != nil {
			return nil, e
		}
	}
	if _, e = os.Stat(filepath.Join(directory, "session.json")); os.IsNotExist(e) {
		if e = s.SaveSession(s.session); e != nil {
			return nil, e
		}
	}
	// The persisted set now contains only desktop windows, never closed history.
	opened := map[string]bool{}
	for _, id := range s.session.OpenWindowIDs {
		opened[id] = true
	}
	for id, w := range s.windows {
		if sessionLoaded && w.Type != "start" && !opened[id] {
			if e = s.DeleteWindow(id); e != nil {
				s.warnings = append(s.warnings, "清理旧窗口记录失败: "+e.Error())
			}
		}
	}
	session := s.session
	session.OpenWindowIDs = []string{}
	for _, id := range s.session.OpenWindowIDs {
		if _, ok := s.windows[id]; ok {
			session.OpenWindowIDs = append(session.OpenWindowIDs, id)
		}
	}
	for id, w := range s.windows {
		if w.Type == "start" && !opened[id] {
			session.OpenWindowIDs = append(session.OpenWindowIDs, id)
		}
	}
	if !reflect.DeepEqual(session, s.session) {
		if e = s.SaveSession(session); e != nil {
			return nil, e
		}
	}
	return s, nil
}
func validateWindow(w model.Window) error {
	if w.SchemaVersion != 1 {
		return fmt.Errorf("unsupported schemaVersion")
	}
	if !uuid.MatchString(w.ID) {
		return fmt.Errorf("invalid window UUID")
	}
	if w.Type != "start" && w.Type != "folder" && w.Type != "collection" {
		return fmt.Errorf("invalid window type")
	}
	if w.TitleMode != "auto" && w.TitleMode != "custom" {
		return fmt.Errorf("invalid title mode")
	}
	if w.Layout != "grid" && w.Layout != "list" {
		return fmt.Errorf("invalid layout")
	}
	if w.Type == "folder" && (w.Layout != "list" || w.RootPath == "" || len(w.Items) > 0) {
		return fmt.Errorf("folder windows require a root and list layout without virtual entries")
	}
	if w.Bounds.Unit != "dip" || w.Bounds.Width < 220 || (!w.Collapsed && w.Bounds.Height < 120) || w.ExpandedSize.Width < 220 || w.ExpandedSize.Height < 120 {
		return fmt.Errorf("invalid bounds")
	}
	if e := appearance.ValidatePatch(w.AppearanceOverrides); e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, v := range w.Items {
		if e := validateEntry(v, false, seen); e != nil {
			return e
		}
	}
	return nil
}
func validateEntry(e model.Entry, nested bool, seen map[string]bool) error {
	if !uuid.MatchString(e.ID) || seen[e.ID] {
		return fmt.Errorf("invalid or duplicate entry ID")
	}
	seen[e.ID] = true
	switch e.Type {
	case "path":
		if e.Path == "" || len(e.Items) > 0 {
			return fmt.Errorf("invalid path entry")
		}
	case "group":
		if nested || len(e.Items) == 0 || strings.TrimSpace(e.Name) == "" {
			return fmt.Errorf("invalid group")
		}
		for _, c := range e.Items {
			if err := validateEntry(c, true, seen); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid entry type")
	}
	return nil
}
func (s *Store) Snapshot() (model.Global, model.Session, []model.Window, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	ws := []model.Window{}
	for _, w := range s.windows {
		ws = append(ws, model.Clone(w))
	}
	sort.Slice(ws, func(i, j int) bool { return ws[i].ID < ws[j].ID })
	return model.Clone(s.global), model.Clone(s.session), ws, append([]string{}, s.warnings...)
}
func (s *Store) Window(id string) (model.Window, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windows[id]
	if !ok {
		return w, os.ErrNotExist
	}
	return model.Clone(w), nil
}
func (s *Store) write(path string, v any, backupPath string) error {
	if s.protected[path] {
		return fmt.Errorf("future schema: refusing to overwrite %s", path)
	}
	b, e := json.MarshalIndent(v, "", "  ")
	if e != nil {
		return e
	}
	if backupPath == "" {
		backupPath = path
	}
	if old, e := os.ReadFile(backupPath); e == nil && validBackup(old, v) {
		if e = atomicWrite(filepath.Join(s.dir, "backups", filepath.Base(path)+".bak"), old); e != nil {
			return e
		}
	}
	return atomicWrite(path, append(b, '\n'))
}
func atomicWrite(path string, b []byte) error {
	f, e := os.CreateTemp(filepath.Dir(path), ".deskgrid-*.tmp")
	if e != nil {
		return e
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, e = f.Write(b); e != nil {
		f.Close()
		return e
	}
	if e = f.Sync(); e != nil {
		f.Close()
		return e
	}
	if e = f.Close(); e != nil {
		return e
	}
	return replaceFile(tmp, path)
}
func (s *Store) SaveWindow(w model.Window) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := validateWindow(w); e != nil {
		return e
	}
	if s.protected[w.ID] {
		return fmt.Errorf("future schema: refusing to overwrite window %s", w.ID)
	}
	old, exists := s.windows[w.ID]
	if exists && (old.Type != w.Type || old.RootPath != w.RootPath) {
		return fmt.Errorf("window identity, type and root are protected")
	}
	if !exists && w.Type == "start" {
		for _, v := range s.windows {
			if v.Type == "start" {
				return fmt.Errorf("start already exists")
			}
		}
	}
	if exists {
		w.Revision = old.Revision + 1
	} else {
		w.Revision = 1
	}
	if w.Items == nil {
		w.Items = []model.Entry{}
	}
	if w.ExpandedPaths == nil {
		w.ExpandedPaths = []string{}
	}
	if w.MenuOverrides == nil {
		w.MenuOverrides = map[string]model.MenuPatch{}
	}
	path := filepath.Join(s.dir, "windows", cleanTitle(w.Title)+"--"+w.ID+".json")
	oldPath := s.files[w.ID]
	if e := s.write(path, w, oldPath); e != nil {
		return e
	}
	s.windows[w.ID] = model.Clone(w)
	s.files[w.ID] = path
	if oldPath != "" && oldPath != path {
		if e := os.Remove(oldPath); e != nil && !os.IsNotExist(e) {
			s.warnings = append(s.warnings, e.Error())
		}
	}
	return nil
}
func (s *Store) DeleteWindow(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	w, ok := s.windows[id]
	if !ok {
		return os.ErrNotExist
	}
	if w.Type == "start" {
		return fmt.Errorf("start cannot be deleted")
	}
	if s.protected[id] {
		return fmt.Errorf("future schema: refusing deletion")
	}
	paths, _ := filepath.Glob(filepath.Join(s.dir, "windows", "*.json"))
	for _, p := range paths {
		b, e := os.ReadFile(p)
		if e != nil {
			return e
		}
		var identity struct {
			ID            string `json:"id"`
			SchemaVersion int    `json:"schemaVersion"`
		}
		if json.Unmarshal(b, &identity) != nil || identity.ID != id {
			continue
		}
		if identity.SchemaVersion > model.SchemaVersion {
			return fmt.Errorf("future schema: refusing deletion")
		}
		if s.protected[p] {
			return fmt.Errorf("future schema: refusing deletion")
		}
		if e := os.Remove(p); e != nil {
			return e
		}
	}
	delete(s.windows, id)
	delete(s.files, id)
	session := model.Clone(s.session)
	session.OpenWindowIDs = []string{}
	for _, windowID := range s.session.OpenWindowIDs {
		if windowID != id {
			session.OpenWindowIDs = append(session.OpenWindowIDs, windowID)
		}
	}
	if e := s.write(filepath.Join(s.dir, "session.json"), session, ""); e != nil {
		return e
	}
	s.session = session
	backups, _ := filepath.Glob(filepath.Join(s.dir, "backups", "*--"+id+".json*"))
	for _, backup := range backups {
		if e := os.Remove(backup); e != nil && !os.IsNotExist(e) {
			s.warnings = append(s.warnings, e.Error())
		}
	}
	return nil
}
func (s *Store) SaveGlobal(g model.Global) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g.SchemaVersion != 1 {
		return errors.New("unsupported schemaVersion")
	}
	if g.Theme != "system" && g.Theme != "light" && g.Theme != "dark" {
		return errors.New("invalid theme")
	}
	if e := appearance.ValidatePatch(g.Appearance); e != nil {
		return e
	}
	if e := s.write(filepath.Join(s.dir, "global.json"), g, ""); e != nil {
		return e
	}
	s.global = model.Clone(g)
	return nil
}
func (s *Store) SaveSession(v model.Session) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if v.SchemaVersion != 1 {
		return errors.New("unsupported schemaVersion")
	}
	seen := map[string]bool{}
	for _, id := range v.OpenWindowIDs {
		if !uuid.MatchString(id) || seen[id] {
			return fmt.Errorf("invalid or duplicate session UUID")
		}
		seen[id] = true
	}
	if v.OpenWindowIDs == nil {
		v.OpenWindowIDs = []string{}
	}
	if e := s.write(filepath.Join(s.dir, "session.json"), v, ""); e != nil {
		return e
	}
	s.session = model.Clone(v)
	return nil
}

func decode(b []byte, dst any) error {
	fresh := reflect.New(reflect.TypeOf(dst).Elem())
	if e := json.Unmarshal(b, fresh.Interface()); e != nil {
		return e
	}
	var e error
	switch v := fresh.Interface().(type) {
	case *model.Window:
		e = validateWindow(*v)
	case *model.Global:
		if v.Theme != "system" && v.Theme != "light" && v.Theme != "dark" {
			e = fmt.Errorf("invalid theme")
		}
		if e == nil {
			e = appearance.ValidatePatch(v.Appearance)
		}
	case *model.Session:
		seen := map[string]bool{}
		for _, id := range v.OpenWindowIDs {
			if !uuid.MatchString(id) || seen[id] {
				e = fmt.Errorf("invalid session UUID")
			}
			seen[id] = true
		}
	}
	if e != nil {
		return e
	}
	reflect.ValueOf(dst).Elem().Set(fresh.Elem())
	return nil
}

func validBackup(b []byte, v any) bool {
	if schema(b) != nil {
		return false
	}
	p := reflect.New(reflect.TypeOf(v))
	return decode(b, p.Interface()) == nil
}
