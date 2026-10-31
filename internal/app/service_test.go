package app

import (
	"encoding/json"
	"os"
	"path/filepath"
	"deskgrid/internal/config"
	"testing"
	"time"
)

func TestCancellationDoesNotWaitForUnrelatedWindowMutation(t *testing.T) {
	s := newTestService(t)
	s.mu.Lock()
	done := make(chan struct{})
	go func() { _, _ = s.Call("CancelTask", raw(map[string]any{"taskId": "missing"})); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		s.mu.Unlock()
		t.Fatal("cancellation blocked by window mutation lock")
	}
	s.mu.Unlock()
}

func TestLoadedDirectoryReceivesNativeChangeNotification(t *testing.T) {
	s := newTestService(t)
	defer s.stopWatches()
	root := t.TempDir()
	v, e := s.Call("CreateWindow", raw(map[string]any{"type": "folder", "path": root}))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	var w struct {
		ID string `json:"id"`
	}
	json.Unmarshal(b, &w)
	events := make(chan string, 1)
	s.Notify = func(id string) {
		select {
		case events <- id:
		default:
		}
	}
	if _, e = s.Call("ListDirectory", raw(map[string]any{"windowId": w.ID, "path": root})); e != nil {
		t.Fatal(e)
	}
	if e = os.WriteFile(filepath.Join(root, "外部新增.txt"), []byte("test"), 0600); e != nil {
		t.Fatal(e)
	}
	select {
	case id := <-events:
		if id != w.ID {
			t.Fatal(id)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("native change never reached window")
	}
}

func newTestService(t *testing.T) *Service {
	t.Helper()
	s, e := config.Open(t.TempDir())
	if e != nil {
		t.Fatal(e)
	}
	return NewService(s, nil)
}
func TestFolderNavigationCannotEscapeRoot(t *testing.T) {
	s := newTestService(t)
	root := t.TempDir()
	outside := t.TempDir()
	v, e := s.Call("CreateWindow", raw(map[string]any{"type": "folder", "path": root}))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	var w struct {
		ID string `json:"id"`
	}
	json.Unmarshal(b, &w)
	_, e = s.Call("Navigate", raw(map[string]any{"windowId": w.ID, "path": outside}))
	if e == nil {
		t.Fatal("escaped bound root")
	}
}
func TestUpdateCannotChangeWindowIdentityOrDiskBinding(t *testing.T) {
	s := newTestService(t)
	_, _, ws, _ := s.Store.Snapshot()
	_, e := s.Call("UpdateWindow", raw(map[string]any{"windowId": ws[0].ID, "patch": map[string]any{"type": "folder", "rootPath": `C:\`}}))
	if e == nil {
		t.Fatal("identity changed")
	}
}
func TestPinAndDeleteWindowNeverDeleteTargets(t *testing.T) {
	s := newTestService(t)
	file := filepath.Join(t.TempDir(), "保留.txt")
	os.WriteFile(file, []byte("keep"), 0600)
	v, e := s.Call("CreateWindow", raw(map[string]any{"type": "collection"}))
	if e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(v)
	var w struct {
		ID string `json:"id"`
	}
	json.Unmarshal(b, &w)
	if _, e = s.Call("Pin", raw(map[string]any{"windowId": w.ID, "paths": []string{file}})); e != nil {
		t.Fatal(e)
	}
	if _, e = s.Call("DeleteWindow", raw(map[string]any{"windowId": w.ID})); e != nil {
		t.Fatal(e)
	}
	b, e = os.ReadFile(file)
	if e != nil || string(b) != "keep" {
		t.Fatal("virtual operation mutated target")
	}
}
func TestUnknownRPCDoesNotExecuteAnything(t *testing.T) {
	s := newTestService(t)
	if _, e := s.Call("exec", raw(map[string]any{"command": "calc.exe"})); e == nil {
		t.Fatal("arbitrary RPC accepted")
	}
}
func TestAppearanceResetActuallyClearsExplicitFields(t *testing.T) {
	s := newTestService(t)
	_, _, ws, _ := s.Store.Snapshot()
	id := ws[0].ID
	for _, patch := range []any{map[string]any{"appearanceOverrides": map[string]any{"titleColor": "#123456"}}, map[string]any{"appearanceOverrides": map[string]any{}}} {
		if _, e := s.Call("UpdateWindow", raw(map[string]any{"windowId": id, "patch": patch})); e != nil {
			t.Fatal(e)
		}
	}
	w, e := s.Store.Window(id)
	if e != nil {
		t.Fatal(e)
	}
	if w.AppearanceOverrides.TitleColor != nil {
		t.Fatal("reset retained explicit colour")
	}
}
func TestSparseMenuPatchesPreserveOtherFieldsAndOrder(t *testing.T) {
	s := newTestService(t)
	_, _, ws, _ := s.Store.Snapshot()
	id := ws[0].ID
	for _, patch := range []any{map[string]any{"actions": map[string]any{"rename": map[string]any{"enabled": false}}, "order": []string{"settings", "rename"}}, map[string]any{"actions": map[string]any{"rename": map[string]any{"name": "显示名称"}, "open": map[string]any{"enabled": false}}}} {
		if _, e := s.Call("SaveMenu", raw(map[string]any{"windowId": id, "context": "file", "patch": patch})); e != nil {
			t.Fatal(e)
		}
	}
	w, _ := s.Store.Window(id)
	m := w.MenuOverrides["file"]
	if len(m.Order) != 2 || m.Actions["rename"].Enabled == nil || *m.Actions["rename"].Enabled || m.Actions["rename"].Name == nil || *m.Actions["rename"].Name != "显示名称" {
		t.Fatalf("sparse patches overwritten %#v", m)
	}
}
func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }
