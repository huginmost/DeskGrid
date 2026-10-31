package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"deskgrid/internal/model"
	"testing"
)

func TestRecoveryAndProtectedIdentity(t *testing.T) {
	d := t.TempDir()
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	_, _, ws, _ := s.Snapshot()
	if len(ws) != 1 || ws[0].Type != "start" {
		t.Fatal(ws)
	}
	if s.DeleteWindow(ws[0].ID) == nil {
		t.Fatal("start deleted")
	}
	w := model.DefaultWindow("collection", "first", "")
	if e = s.SaveWindow(w); e != nil {
		t.Fatal(e)
	}
	w.Title = "second"
	if e = s.SaveWindow(w); e != nil {
		t.Fatal(e)
	}
	w.Type = "folder"
	if s.SaveWindow(w) == nil {
		t.Fatal("type changed")
	}
	files, _ := filepath.Glob(filepath.Join(d, "windows", "second--*.json"))
	if len(files) != 1 {
		t.Fatal(files)
	}
	os.WriteFile(files[0], []byte("broken"), 0600)
	r, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	got, e := r.Window(w.ID)
	if e != nil || got.Title != "first" {
		t.Fatal(got, e)
	}
	_, _, copy, _ := r.Snapshot()
	copy[0].Title = "mutated"
	_, _, again, _ := r.Snapshot()
	if again[0].Title == "mutated" {
		t.Fatal("snapshot aliased")
	}
}

func TestFutureSchemaCannotBeOverwritten(t *testing.T) {
	d := t.TempDir()
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	g, _, _, _ := s.Snapshot()
	future := []byte(`{"schemaVersion":99,"theme":"dark"}`)
	if e = os.WriteFile(filepath.Join(d, "global.json"), future, 0600); e != nil {
		t.Fatal(e)
	}
	s, e = Open(d)
	if e != nil {
		t.Fatal(e)
	}
	if s.SaveGlobal(g) == nil {
		t.Fatal("future global overwritten")
	}
	b, _ := os.ReadFile(filepath.Join(d, "global.json"))
	if string(b) != string(future) {
		t.Fatal("future bytes changed")
	}
}
func TestDuplicateRevisionAndNestedGroups(t *testing.T) {
	d := t.TempDir()
	s, _ := Open(d)
	w := model.DefaultWindow("collection", "old", "")
	if e := s.SaveWindow(w); e != nil {
		t.Fatal(e)
	}
	w, _ = s.Window(w.ID)
	w.Title = "new"
	w.Revision = 9
	b, _ := json.Marshal(w)
	os.WriteFile(filepath.Join(d, "windows", "duplicate.json"), b, 0600)
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	got, _ := s.Window(w.ID)
	if got.Title != "new" || got.Revision != 9 {
		t.Fatal(got)
	}
	w.Items = []model.Entry{{ID: model.NewID(), Type: "group", Name: "a", Items: []model.Entry{{ID: model.NewID(), Type: "group", Name: "b"}}}}
	if s.SaveWindow(w) == nil {
		t.Fatal("nested groups accepted")
	}
}
func TestMalformedGlobalDoesNotLeakPartialValues(t *testing.T) {
	d := t.TempDir()
	os.WriteFile(filepath.Join(d, "global.json"), []byte(`{"schemaVersion":1,"theme":"bogus","appearance":{"opacity":3}}`), 0600)
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	g, _, _, warnings := s.Snapshot()
	if g.Theme != "system" || len(warnings) == 0 {
		t.Fatal(g, warnings)
	}
	matches, _ := filepath.Glob(filepath.Join(d, "backups", "*.corrupt-*"))
	if len(matches) != 1 {
		t.Fatal("corrupt config not isolated", matches)
	}
}
func TestFutureWindowIDCannotBeReused(t *testing.T) {
	d := t.TempDir()
	w := model.DefaultWindow("collection", "future", "")
	w.SchemaVersion = 9
	os.MkdirAll(filepath.Join(d, "windows"), 0700)
	b, _ := json.Marshal(w)
	os.WriteFile(filepath.Join(d, "windows", "future--"+w.ID+".json"), b, 0600)
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	w.SchemaVersion = 1
	w.Title = "renamed"
	if s.SaveWindow(w) == nil {
		t.Fatal("future window overwritten by rename")
	}
}
func TestCorruptionDoesNotReplaceLastValidBackup(t *testing.T) {
	d := t.TempDir()
	s, _ := Open(d)
	w := model.DefaultWindow("collection", "same", "")
	s.SaveWindow(w)
	s.SaveWindow(w)
	p := filepath.Join(d, "windows", "same--"+w.ID+".json")
	bad, _ := json.Marshal(map[string]any{"schemaVersion": 1, "id": w.ID, "type": "bad"})
	os.WriteFile(p, bad, 0600)
	if e := s.SaveWindow(w); e != nil {
		t.Fatal(e)
	}
	os.WriteFile(p, []byte("broken"), 0600)
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	got, e := s.Window(w.ID)
	if e != nil || got.Type != "collection" {
		t.Fatal(got, e)
	}
}
func TestDeletingWindowRemovesAllSameIdentityFiles(t *testing.T) {
	d := t.TempDir()
	s, _ := Open(d)
	w := model.DefaultWindow("collection", "one", "")
	s.SaveWindow(w)
	w, _ = s.Window(w.ID)
	b, _ := json.Marshal(w)
	os.WriteFile(filepath.Join(d, "windows", "duplicate.json"), b, 0600)
	if e := s.DeleteWindow(w.ID); e != nil {
		t.Fatal(e)
	}
	s, e := Open(d)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Window(w.ID); e == nil {
		t.Fatal("duplicate resurrected deleted window")
	}
}
