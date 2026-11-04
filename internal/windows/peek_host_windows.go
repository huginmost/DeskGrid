//go:build windows

package windows

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	win "golang.org/x/sys/windows"
)

type peekHost struct {
	window, child uintptr
	focus         func(bool)
}

var peekChildren sync.Map   // content HWND -> *peekHost
var peekContainers sync.Map // top-level HWND -> *peekHost
var peekClassOnce sync.Once
var peekClassError error
var peekClassName = win.StringToUTF16Ptr("DeskGrid.TemporaryForeground")

func peekRoot(hwnd uintptr) uintptr {
	if value, ok := peekChildren.Load(hwnd); ok {
		return value.(*peekHost).window
	}
	return hwnd
}

var peekHostProc = syscall.NewCallback(func(hwnd, message, wparam, lparam uintptr) uintptr {
	if message == 0x83 { // WM_NCCALCSIZE: frameless, with native resize support.
		return 0
	}
	if value, ok := peekContainers.Load(hwnd); ok {
		host := value.(*peekHost)
		if host.child != 0 && (message == 0x84 || message == 0x112 || message == 0xa1) {
			style, _, _ := user32.NewProc("GetWindowLongPtrW").Call(host.child, ^uintptr(15))
			if style&0x40000 == 0 { // The content controls resize permission for both HWNDs.
				if message == 0x84 { // WM_NCHITTEST
					return 1 // HTCLIENT: no edge/corner sizing while pinned or animating.
				}
				if message == 0x112 && wparam&0xfff0 == 0xf000 || message == 0xa1 && wparam >= 10 && wparam <= 17 {
					return 0 // SC_SIZE / resize-border mouse down.
				}
			}
		}
		switch message {
		case 0x6: // WM_ACTIVATE
			active := wparam&0xffff != 0
			if active && host.child != 0 {
				user32.NewProc("SetFocus").Call(host.child)
			}
			if host.focus != nil {
				host.focus(active)
			}
		case 0x5: // WM_SIZE: keep the existing WebView filling its host.
			if host.child != 0 && wparam != 1 {
				var rect nativeRect
				user32.NewProc("GetClientRect").Call(hwnd, uintptr(unsafe.Pointer(&rect)))
				user32.NewProc("SetWindowPos").Call(host.child, 0, 0, 0, uintptr(rect.Right), uintptr(rect.Bottom), 0x4|0x10)
			}
		case 0x3: // WM_MOVE: Wails must update WebView position and saved geometry.
			if host.child != 0 {
				user32.NewProc("SendMessageW").Call(host.child, message, wparam, lparam)
			}
		case 0x216: // Forward fixed-window protection; free moves are not adjusted.
			if host.child != 0 {
				r, _, _ := user32.NewProc("SendMessageW").Call(host.child, message, wparam, lparam)
				return r
			}
		case 0x231, 0x232: // Preserve user move/resize sessions for layout persistence.
			if host.child != 0 {
				user32.NewProc("SendMessageW").Call(host.child, message, wparam, lparam)
			}
		case 0x24, 0x84: // WM_GETMINMAXINFO / WM_NCHITTEST
			if host.child != 0 {
				r, _, _ := user32.NewProc("SendMessageW").Call(host.child, message, wparam, lparam)
				return r
			}
		case 0x2e0: // WM_DPICHANGED
			rect := (*nativeRect)(unsafe.Pointer(lparam))
			user32.NewProc("SetWindowPos").Call(hwnd, 0, uintptr(rect.Left), uintptr(rect.Top), uintptr(rect.Right-rect.Left), uintptr(rect.Bottom-rect.Top), 0x4|0x10)
			return 0
		case 0x10: // WM_CLOSE: preserve the original start/locked-window protection.
			if host.child != 0 {
				user32.NewProc("PostMessageW").Call(host.child, message, 0, 0)
			} else {
				user32.NewProc("DestroyWindow").Call(hwnd)
			}
			return 0
		case 0x14: // WM_ERASEBKGND: the content child covers the client area.
			return 1
		case 0x82: // WM_NCDESTROY
			if host.child != 0 {
				user32.NewProc("SendMessageW").Call(host.child, 0x232, 0, 0)
			}
			peekContainers.Delete(hwnd)
			peekChildren.Delete(host.child)
		}
	}
	r, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, message, wparam, lparam)
	return r
})

func createPeekHost(bounds nativeRect, focus func(bool)) (*peekHost, error) {
	module, _, _ := win.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	peekClassOnce.Do(func() {
		cursor, _, _ := user32.NewProc("LoadCursorW").Call(0, 32512)
		class := struct {
			Size, Style                        uint32
			Procedure                          uintptr
			ClassExtra, WindowExtra            int32
			Instance, Icon, Cursor, Background uintptr
			Menu, Name                         *uint16
			SmallIcon                          uintptr
		}{Procedure: peekHostProc, Instance: module, Cursor: cursor, Name: peekClassName}
		class.Size = uint32(unsafe.Sizeof(class))
		if atom, _, err := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class))); atom == 0 {
			peekClassError = fmt.Errorf("注册临时前置容器: %w", err)
		}
	})
	if peekClassError != nil {
		return nil, peekClassError
	}
	// Create a fresh, ownerless top-level HWND. Reusing a window previously
	// parented to Explorer can retain desktop z-order restrictions after detach.
	hwnd, _, err := user32.NewProc("CreateWindowExW").Call(0x80088, uintptr(unsafe.Pointer(peekClassName)), 0, 0x82040000, uintptr(bounds.Left), uintptr(bounds.Top), uintptr(bounds.Right-bounds.Left), uintptr(bounds.Bottom-bounds.Top), 0, 0, module, 0)
	if hwnd == 0 {
		return nil, fmt.Errorf("创建临时前置容器: %w", err)
	}
	host := &peekHost{window: hwnd, focus: focus}
	peekContainers.Store(hwnd, host)
	return host, nil
}

// Hide both the WebView and its temporary native host so a retained hidden
// start window cannot leave an empty topmost input target during Win+A.
func HideNativeWindow(hwnd uintptr) {
	root := peekRoot(hwnd)
	user32.NewProc("ShowWindow").Call(root, 0)
	if root != hwnd {
		user32.NewProc("ShowWindow").Call(hwnd, 0)
	}
}

// PeekMessage forwards native edge resizing and disposes of the temporary
// container if Wails destroys its content window while peek is active.
func PeekMessage(hwnd uintptr, message uint32, wparam, lparam uintptr) (uintptr, bool) {
	value, ok := peekChildren.Load(hwnd)
	if !ok {
		return 0, false
	}
	host := value.(*peekHost)
	if message == 0xa1 && (wparam == 2 || wparam >= 10 && wparam <= 17) {
		user32.NewProc("PostMessageW").Call(host.window, uintptr(message), wparam, lparam)
		return 0, true
	}
	if message == 0x82 {
		peekChildren.Delete(hwnd)
		host.child = 0
		user32.NewProc("PostMessageW").Call(host.window, 0x10, 0, 0)
	}
	return 0, false
}
