package filesystem

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func put(t *testing.T, p, s string) {
	t.Helper()
	if e := os.WriteFile(p, []byte(s), 0600); e != nil {
		t.Fatal(e)
	}
}
func done(t *testing.T, m *Manager, id string) Task {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		for _, v := range m.Tasks() {
			if v.ID == id && (v.Status == "completed" || v.Status == "failed" || v.Status == "cancelled") {
				return v
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("task timeout")
	return Task{}
}
func TestBoundaryAndNames(t *testing.T) {
	r := t.TempDir()
	if Within(r, r+"-other") {
		t.Fatal("prefix escape")
	}
	for _, n := range []string{"..", "a/b", "CON.txt", "x.", "x ", "LPT1", "a:b"} {
		if ValidateName(n) == nil {
			t.Errorf("accepted %q", n)
		}
	}
	if e := ValidateName("中文 文件.txt"); e != nil {
		t.Fatal(e)
	}
	if e := Create(r, filepath.Dir(r), "escape", false); e == nil {
		t.Fatal("create escaped")
	}
}
func TestListingNaturalAndPagination(t *testing.T) {
	r := t.TempDir()
	os.Mkdir(filepath.Join(r, "z"), 0700)
	put(t, filepath.Join(r, "10.txt"), "a")
	put(t, filepath.Join(r, "2.txt"), "b")
	l, e := List(r, true, 1, 1)
	if e != nil || l.Total != 3 || len(l.Entries) != 1 || l.Entries[0].Name != "2.txt" {
		t.Fatalf("%+v %v", l, e)
	}
}
func TestRejectSelfAndDescendantBeforeMutation(t *testing.T) {
	r := t.TempDir()
	src := filepath.Join(r, "a")
	os.Mkdir(src, 0700)
	sub := filepath.Join(src, "sub")
	os.Mkdir(sub, 0700)
	m := NewManager()
	for _, d := range []string{r, sub} {
		if _, e := m.Start([]string{src}, d, "move", "replace"); e == nil {
			t.Fatal("unsafe transfer accepted")
		}
	}
	if _, e := os.Stat(src); e != nil {
		t.Fatal(e)
	}
}
func TestCopyMergeAndMoveSkipPreservesSource(t *testing.T) {
	r := t.TempDir()
	s := filepath.Join(r, "s")
	d := filepath.Join(r, "d")
	os.Mkdir(s, 0700)
	os.Mkdir(d, 0700)
	os.Mkdir(filepath.Join(s, "tree"), 0700)
	os.Mkdir(filepath.Join(d, "tree"), 0700)
	put(t, filepath.Join(s, "tree", "new"), "new")
	put(t, filepath.Join(d, "tree", "old"), "old")
	m := NewManager()
	v, e := m.Start([]string{filepath.Join(s, "tree")}, d, "copy", "replace")
	if e != nil {
		t.Fatal(e)
	}
	if x := done(t, m, v.ID); x.Status != "completed" {
		t.Fatal(x)
	}
	for _, n := range []string{"new", "old"} {
		if _, e := os.Stat(filepath.Join(d, "tree", n)); e != nil {
			t.Fatal(e)
		}
	}
	put(t, filepath.Join(s, "same"), "source")
	put(t, filepath.Join(d, "same"), "target")
	v, e = m.Start([]string{filepath.Join(s, "same")}, d, "move", "skip")
	if e != nil {
		t.Fatal(e)
	}
	done(t, m, v.ID)
	b, _ := os.ReadFile(filepath.Join(s, "same"))
	if string(b) != "source" {
		t.Fatal("skipped move deleted source")
	}
}
func TestConflictCancelAndKeepBoth(t *testing.T) {
	r := t.TempDir()
	s := filepath.Join(r, "s")
	d := filepath.Join(r, "d")
	os.Mkdir(s, 0700)
	os.Mkdir(d, 0700)
	put(t, filepath.Join(s, "x"), "source")
	put(t, filepath.Join(d, "x"), "target")
	m := NewManager()
	v, e := m.Start([]string{filepath.Join(s, "x")}, d, "move", "ask")
	if e != nil {
		t.Fatal(e)
	}
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		if m.Tasks()[0].Status == "conflict" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if e = m.Cancel(v.ID); e != nil {
		t.Fatal(e)
	}
	if x := done(t, m, v.ID); x.Status != "cancelled" {
		t.Fatal(x)
	}
	b, _ := os.ReadFile(filepath.Join(s, "x"))
	if string(b) != "source" {
		t.Fatal("cancel deleted source")
	}
	v, e = m.Start([]string{filepath.Join(s, "x")}, d, "copy", "keepBoth")
	if e != nil {
		t.Fatal(e)
	}
	done(t, m, v.ID)
	b, _ = os.ReadFile(filepath.Join(d, "x (2)"))
	if string(b) != "source" {
		t.Fatal("missing independent copy")
	}
}

// Catches deletion of move sources after a child copy fails.
func TestPartialMoveFailurePreservesUncopiedSource(t *testing.T) {
	r := t.TempDir()
	s := filepath.Join(r, "source")
	d := filepath.Join(r, "dest")
	os.Mkdir(s, 0700)
	os.Mkdir(d, 0700)
	os.Mkdir(filepath.Join(s, "tree"), 0700)
	os.Mkdir(filepath.Join(d, "tree"), 0700)
	put(t, filepath.Join(s, "tree", "a"), "good")
	os.Mkdir(filepath.Join(s, "tree", "b"), 0700)
	put(t, filepath.Join(s, "tree", "b", "precious"), "keep")
	put(t, filepath.Join(d, "tree", "b"), "existing file")
	m := NewManager()
	v, e := m.Start([]string{filepath.Join(s, "tree")}, d, "move", "replace")
	if e != nil {
		t.Fatal(e)
	}
	x := done(t, m, v.ID)
	if x.Status != "failed" {
		t.Fatal(x)
	}
	b, _ := os.ReadFile(filepath.Join(s, "tree", "b", "precious"))
	if string(b) != "keep" {
		t.Fatal("failed move lost source")
	}
	b, _ = os.ReadFile(filepath.Join(d, "tree", "a"))
	if string(b) != "good" {
		t.Fatal("successful partial item not preserved")
	}
	if x.Completed != 1 || x.Total != 1 {
		t.Fatalf("top-level progress: %+v", x)
	}
}
func TestReplacementAndResolve(t *testing.T) {
	r := t.TempDir()
	s := filepath.Join(r, "s")
	d := filepath.Join(r, "d")
	os.Mkdir(s, 0700)
	os.Mkdir(d, 0700)
	put(t, filepath.Join(s, "x"), "new")
	put(t, filepath.Join(d, "x"), "old")
	m := NewManager()
	v, e := m.Start([]string{filepath.Join(s, "x")}, d, "copy", "ask")
	if e != nil {
		t.Fatal(e)
	}
	end := time.Now().Add(time.Second)
	for time.Now().Before(end) {
		if m.Tasks()[0].Status == "conflict" {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if e = m.Resolve(v.ID, "replace", true); e != nil {
		t.Fatal(e)
	}
	x := done(t, m, v.ID)
	if x.Status != "completed" {
		t.Fatal(x)
	}
	b, _ := os.ReadFile(filepath.Join(d, "x"))
	if string(b) != "new" {
		t.Fatal("replace not committed")
	}
	b, _ = os.ReadFile(filepath.Join(s, "x"))
	if string(b) != "new" {
		t.Fatal("copy changed source")
	}
}
func TestReparseConfinement(t *testing.T) {
	r := t.TempDir()
	outside := t.TempDir()
	p := filepath.Join(r, "link")
	if e := os.Symlink(outside, p); e != nil {
		t.Skipf("symlink privilege unavailable: %v", e)
	}
	if e := Create(r, p, "escape", false); e == nil {
		t.Fatal("created outside root through link")
	}
	if _, e := List(p, true, 0, 0); e == nil {
		t.Fatal("expanded link")
	}
}
func TestWatchNotifiesLoadedDirectory(t *testing.T) {
	r := t.TempDir()
	events := make(chan struct{}, 1)
	cancel, e := Watch([]string{r}, func() {
		select {
		case events <- struct{}{}:
		default:
		}
	})
	if e != nil {
		t.Fatal(e)
	}
	defer cancel()
	put(t, filepath.Join(r, "new"), "changed")
	select {
	case <-events:
	case <-time.After(3 * time.Second):
		t.Fatal("no directory notification")
	}
}
func TestRenameNeverOverwritesAndCreateExclusive(t *testing.T) {
	r := t.TempDir()
	a := filepath.Join(r, "a")
	b := filepath.Join(r, "b")
	put(t, a, "source")
	put(t, b, "target")
	if e := Rename(r, a, "b"); e == nil {
		t.Fatal("overwrite rename accepted")
	}
	if e := Create(r, r, "b", false); e == nil {
		t.Fatal("overwrite create accepted")
	}
	if e := Rename(r, a, "中文.txt"); e != nil {
		t.Fatal(e)
	}
	got, _ := os.ReadFile(filepath.Join(r, "中文.txt"))
	if string(got) != "source" {
		t.Fatal("rename lost data")
	}
	got, _ = os.ReadFile(b)
	if string(got) != "target" {
		t.Fatal("existing target changed")
	}
	if e := Create(r, r, "folder", true); e != nil {
		t.Fatal(e)
	}
	if i, e := os.Stat(filepath.Join(r, "folder")); e != nil || !i.IsDir() {
		t.Fatal("directory not created")
	}
}
