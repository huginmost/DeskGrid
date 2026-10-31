//go:build windows

package filesystem

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestAlternateStreamsSurviveCopyAndMoveReplacement(t *testing.T) {
	r := t.TempDir()
	s := filepath.Join(r, "s")
	d := filepath.Join(r, "d")
	os.Mkdir(s, 0700)
	os.Mkdir(d, 0700)
	source := filepath.Join(s, "file")
	put(t, source, "main")
	if e := os.WriteFile(source+":secret", []byte("precious"), 0600); e != nil {
		t.Skipf("ADS unsupported: %v", e)
	}
	m := NewManager()
	v, e := m.Start([]string{source}, d, "copy", "replace")
	if e != nil {
		t.Fatal(e)
	}
	if x := done(t, m, v.ID); x.Status != "completed" {
		t.Fatal(x)
	}
	target := filepath.Join(d, "file")
	b, e := os.ReadFile(target + ":secret")
	if e != nil || string(b) != "precious" {
		t.Fatalf("copy lost alternate stream: %q %v", b, e)
	}
	put(t, source+":secret", "updated")
	v, e = m.Start([]string{source}, d, "move", "replace")
	if e != nil {
		t.Fatal(e)
	}
	if x := done(t, m, v.ID); x.Status != "completed" {
		t.Fatal(x)
	}
	b, e = os.ReadFile(target + ":secret")
	if e != nil || string(b) != "updated" {
		t.Fatalf("move lost alternate stream: %q %v", b, e)
	}
	if _, e = os.Stat(source); !os.IsNotExist(e) {
		t.Fatal("move source remains")
	}
}
func TestNativeCopyCancellationKeepsSource(t *testing.T) {
	r := t.TempDir()
	s := filepath.Join(r, "source")
	d := filepath.Join(r, "target")
	f, e := os.Create(s)
	if e != nil {
		t.Fatal(e)
	}
	if e = f.Truncate(32 * 1024 * 1024); e != nil {
		t.Fatal(e)
	}
	f.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var progress int64
	e = copyFile(ctx, s, d, func(n int64) { progress += n; cancel() })
	if !errors.Is(e, context.Canceled) {
		t.Fatalf("copy should cancel after progress: %v (%d bytes)", e, progress)
	}
	if i, e := os.Stat(s); e != nil || i.Size() != 32*1024*1024 {
		t.Fatal("cancel changed source")
	}
	if _, e := os.Stat(d); !os.IsNotExist(e) {
		t.Fatal("cancel left partial target")
	}
}
func TestNativeCopyExclusiveTarget(t *testing.T) {
	r := t.TempDir()
	s := filepath.Join(r, "source")
	d := filepath.Join(r, "target")
	put(t, s, "source")
	put(t, d, "target")
	if e := copyFile(context.Background(), s, d, func(int64) {}); e == nil {
		t.Fatal("overwrote target")
	}
	b, e := os.ReadFile(d)
	if e != nil || string(b) != "target" {
		t.Fatal("exclusive target was changed or removed")
	}
}
