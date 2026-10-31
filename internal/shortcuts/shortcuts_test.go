package shortcuts

import (
	"os"
	"path/filepath"
	"deskgrid/internal/model"
	"testing"
)

func TestGroupingAndIsolation(t *testing.T) {
	w := model.DefaultWindow("collection", "x", "")
	if _, e := Pin(&w, []string{"a", "b"}, ""); e != nil {
		t.Fatal(e)
	}
	a, b := w.Items[0].ID, w.Items[1].ID
	if e := Mutate(&w, "group", []string{a, b}, "", "", 0); e != nil {
		t.Fatal(e)
	}
	if len(w.Items) != 1 || w.Items[0].Name != "应用文件夹1" {
		t.Fatal(w.Items)
	}
	g := w.Items[0].ID
	if e := Mutate(&w, "remove", []string{a}, "", "", 0); e != nil {
		t.Fatal(e)
	}
	if len(w.Items[0].Items) != 1 {
		t.Fatal("one item group removed")
	}
	if e := Mutate(&w, "moveToGroup", []string{g}, "", g, 0); e == nil {
		t.Fatal("nesting accepted")
	}
	if e := Mutate(&w, "remove", []string{b}, "", "", 0); e != nil || len(w.Items) != 0 {
		t.Fatal(w.Items, e)
	}
}
func TestUngroupRejectsDuplicateWithoutChangingWindow(t *testing.T) {
	w := model.DefaultWindow("collection", "x", "")
	Pin(&w, []string{"a", "b"}, "")
	ids := []string{w.Items[0].ID, w.Items[1].ID}
	Mutate(&w, "group", ids, "", "", 0)
	g := w.Items[0].ID
	Pin(&w, []string{"a"}, "")
	if e := Mutate(&w, "ungroup", []string{g}, "", "", 0); e == nil {
		t.Fatal("duplicate target created")
	}
	if len(w.Items) != 2 || len(w.Items[0].Items) != 2 {
		t.Fatal("partial mutation", w.Items)
	}
}
func TestTransferMoveAndCopyPreserveRealFile(t *testing.T) {
	d := t.TempDir()
	path := filepath.Join(d, "target.txt")
	os.WriteFile(path, []byte("unchanged"), 0600)
	s := model.DefaultWindow("collection", "s", "")
	target := model.DefaultWindow("collection", "t", "")
	Pin(&s, []string{path}, "")
	id := s.Items[0].ID
	if e := Transfer(&s, &target, []string{id}, true, "", 0); e != nil {
		t.Fatal(e)
	}
	if len(s.Items) != 1 || target.Items[0].ID == id {
		t.Fatal("copy failed")
	}
	b, e := os.ReadFile(path)
	if e != nil || string(b) != "unchanged" {
		t.Fatal("disk target changed")
	}
	if e := Transfer(&s, &target, []string{id}, false, "", 0); e == nil || len(s.Items) != 1 {
		t.Fatal("duplicate transfer changed source")
	}
}
