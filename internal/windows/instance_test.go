//go:build windows

package windows

import (
	"testing"
	"time"
)

func TestSingleInstanceOwnsMutexAndForwardsRequest(t *testing.T) {
	key := "test-" + t.Name() + time.Now().Format("150405.000000000")
	ch := make(chan Request, 1)
	first, primary, e := Acquire(key, Request{}, func(r Request) { ch <- r })
	if e != nil || !primary {
		t.Fatalf("first %v %v", primary, e)
	}
	defer first.Close()
	second, primary, e := Acquire(key, Request{Paths: []string{`D:\中文 空格\a.txt`}}, nil)
	if second != nil {
		defer second.Close()
	}
	if e != nil || primary {
		t.Fatalf("second %v %v", primary, e)
	}
	select {
	case r := <-ch:
		if len(r.Paths) != 1 || r.Paths[0] != `D:\中文 空格\a.txt` {
			t.Fatal(r)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("activation lost")
	}
}
func TestClampKeepsTitleOnVisibleWorkArea(t *testing.T) {
	x, y, w, h := Clamp(5000, -800, 400, 300, []WorkArea{{0, 0, 1920, 1040}})
	if x < 0 || y < 0 || x+w > 1920 || y+h > 1040 {
		t.Fatalf("offscreen %d %d %d %d", x, y, w, h)
	}
}
