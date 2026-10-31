//go:build windows

package shell

import (
	"fmt"
	"golang.org/x/sys/windows"
	"sync"
	"time"
)

var driveCache = struct {
	sync.Mutex
	items     []Drive
	pending   map[string]bool
	refreshed time.Time
}{pending: map[string]bool{}}

// Drives returns immediately from a cache. Each volume is queried independently,
// so an offline mapped drive cannot block cancellation or other windows.
func Drives() []Drive {
	driveCache.Lock()
	defer driveCache.Unlock()
	if time.Since(driveCache.refreshed) < 3*time.Second {
		return append([]Drive{}, driveCache.items...)
	}
	driveCache.refreshed = time.Now()
	mask, e := windows.GetLogicalDrives()
	if e != nil {
		return append([]Drive{}, driveCache.items...)
	}
	next := []Drive{}
	for i := 0; i < 26; i++ {
		if mask&(1<<i) == 0 {
			continue
		}
		path := fmt.Sprintf("%c:\\", 'A'+i)
		d := Drive{Path: path, Name: fmt.Sprintf("本地磁盘 (%c:)", 'A'+i)}
		for _, old := range driveCache.items {
			if old.Path == path {
				d = old
				break
			}
		}
		next = append(next, d)
		if driveCache.pending[path] {
			continue
		}
		driveCache.pending[path] = true
		go func(path string) {
			var label [261]uint16
			d := Drive{Path: path}
			e := windows.GetVolumeInformation(u16(path), &label[0], 261, nil, nil, nil, nil, 0)
			name := windows.UTF16ToString(label[:])
			if name == "" {
				name = "本地磁盘"
			}
			d.Name = fmt.Sprintf("%s (%s)", name, path[:2])
			if e != nil {
				d.Error = e.Error()
			} else {
				var available uint64
				if e = windows.GetDiskFreeSpaceEx(u16(path), &available, &d.Total, &d.Free); e != nil {
					d.Error = e.Error()
				}
			}
			driveCache.Lock()
			delete(driveCache.pending, path)
			for j := range driveCache.items {
				if driveCache.items[j].Path == path {
					driveCache.items[j] = d
					break
				}
			}
			driveCache.Unlock()
		}(path)
	}
	driveCache.items = next
	return append([]Drive{}, next...)
}
