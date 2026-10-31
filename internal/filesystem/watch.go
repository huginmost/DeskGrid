package filesystem

import (
	"errors"
	"github.com/fsnotify/fsnotify"
	"os"
	"sync"
)

// Watch observes exactly the loaded directories supplied, never recursively.
// notify runs on a watcher goroutine and should return promptly.
func Watch(paths []string, notify func()) (func(), error) {
	if notify == nil {
		return nil, errors.New("notification callback is required")
	}
	w, e := fsnotify.NewWatcher()
	if e != nil {
		return nil, e
	}
	for _, p := range paths {
		i, e := os.Lstat(p)
		if e != nil {
			w.Close()
			return nil, e
		}
		if !i.IsDir() || isLink(p, i) {
			w.Close()
			return nil, errors.New("watch requires a real directory")
		}
		if e = w.Add(p); e != nil {
			w.Close()
			return nil, e
		}
	}
	var once sync.Once
	cancel := func() { once.Do(func() { w.Close() }) }
	go func() {
		defer cancel()
		for {
			select {
			case _, ok := <-w.Events:
				if !ok {
					return
				}
				notify()
			case _, ok := <-w.Errors:
				if !ok {
					return
				}
				notify()
			}
		}
	}()
	return cancel, nil
}
