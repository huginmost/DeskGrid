//go:build windows

package filesystem

import (
	"context"
	"errors"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var kernel = windows.NewLazySystemDLL("kernel32.dll")
var copyFileEx = kernel.NewProc("CopyFileExW")
var firstStream = kernel.NewProc("FindFirstStreamW")
var nextStream = kernel.NewProc("FindNextStreamW")

type streamData struct {
	Size int64
	Name [296]uint16
}

func hasNamedStreams(p string) (bool, error) {
	u, e := windows.UTF16PtrFromString(p)
	if e != nil {
		return false, e
	}
	var d streamData
	h, _, err := firstStream.Call(uintptr(unsafe.Pointer(u)), 0, uintptr(unsafe.Pointer(&d)), 0)
	if h == ^uintptr(0) {
		if err == syscall.ERROR_HANDLE_EOF {
			return false, nil
		}
		return false, err
	}
	defer windows.FindClose(windows.Handle(h))
	for {
		if windows.UTF16ToString(d.Name[:]) != "::$DATA" {
			return true, nil
		}
		r, _, err := nextStream.Call(h, uintptr(unsafe.Pointer(&d)))
		if r == 0 {
			if err == syscall.ERROR_HANDLE_EOF {
				return false, nil
			}
			return false, err
		}
	}
}
func streamsSupported(dir string) (bool, error) {
	u, e := windows.UTF16PtrFromString(dir)
	if e != nil {
		return false, e
	}
	buf := make([]uint16, 32768)
	if e = windows.GetVolumePathName(u, &buf[0], uint32(len(buf))); e != nil {
		return false, e
	}
	var flags uint32
	e = windows.GetVolumeInformation(&buf[0], nil, 0, nil, nil, &flags, nil, 0)
	return flags&0x40000 != 0, e
}

type copyProgress struct {
	ctx      context.Context
	progress func(int64)
	last     int64
}

var copyStates sync.Map
var copySequence atomic.Uint64

// LARGE_INTEGER callback arguments occupy one machine word on Windows 64-bit.
var copyCallback = syscall.NewCallback(func(total, transferred, streamSize, streamTransferred, streamNumber, reason, source, dest, data uintptr) uintptr {
	v, ok := copyStates.Load(data)
	if !ok {
		return 1
	}
	s := v.(*copyProgress)
	if s.ctx.Err() != nil {
		return 1
	}
	n := int64(transferred)
	if n > s.last {
		s.progress(n - s.last)
		s.last = n
	}
	return 0
})

func copyFile(ctx context.Context, s, d string, onProgress func(int64)) error {
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		return errors.New("native copy progress requires Windows 64-bit")
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	supported, e := streamsSupported(filepath.Dir(s))
	if e != nil {
		return e
	}
	ads := false
	if supported {
		ads, e = hasNamedStreams(s)
	}
	if e != nil {
		return e
	}
	if ads {
		ok, e := streamsSupported(filepath.Dir(d))
		if e != nil {
			return e
		}
		if !ok {
			return errors.New("destination volume cannot preserve alternate data streams")
		}
	}
	from, e := windows.UTF16PtrFromString(s)
	if e != nil {
		return e
	}
	to, e := windows.UTF16PtrFromString(d)
	if e != nil {
		return e
	}
	id := uintptr(copySequence.Add(1))
	copyStates.Store(id, &copyProgress{ctx: ctx, progress: onProgress})
	defer copyStates.Delete(id)
	r, _, err := copyFileEx.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), copyCallback, id, 0, 1)
	runtime.KeepAlive(from)
	runtime.KeepAlive(to)
	if r == 0 {
		if err != windows.ERROR_FILE_EXISTS && err != windows.ERROR_ALREADY_EXISTS {
			os.Remove(d)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return err
	}
	return nil
}
