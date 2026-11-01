//go:build windows

package windows

import (
	"math"
	"sync"
	"syscall"
	"unsafe"

	win "golang.org/x/sys/windows"
)

// AlignmentGuides is used only on the UI thread. Normal guides are children of
// Explorer's desktop parent; temporary Win+A guides are top-level and topmost.
type AlignmentGuides struct {
	horizontal, vertical uintptr
	parent               uintptr
	foreground           bool
	moving               uintptr
	start                nativeRect
	cursor               nativePoint
	mouseDrag            bool
}

var alignmentClassOnce sync.Once
var alignmentClassOK bool
var alignmentClassName = win.StringToUTF16Ptr("DeskGrid.AlignmentGuide")

var alignmentGuideProc = syscall.NewCallback(func(hwnd, message, wparam, lparam uintptr) uintptr {
	switch message {
	case 0x84: // HTTRANSPARENT, including when a guide crosses a sibling window.
		return ^uintptr(0)
	case 0x21: // MA_NOACTIVATE
		return 3
	}
	r, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, message, wparam, lparam)
	return r
})

func createAlignmentGuide(parent uintptr, foreground bool) uintptr {
	module, _, _ := win.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
	alignmentClassOnce.Do(func() {
		brush, _, _ := win.NewLazySystemDLL("gdi32.dll").NewProc("CreateSolidBrush").Call(0x00e5a15e)
		class := struct {
			Size, Style                        uint32
			Procedure                          uintptr
			ClassExtra, WindowExtra            int32
			Instance, Icon, Cursor, Background uintptr
			Menu, Name                         *uint16
			SmallIcon                          uintptr
		}{Procedure: alignmentGuideProc, Instance: module, Background: brush, Name: alignmentClassName}
		class.Size = uint32(unsafe.Sizeof(class))
		atom, _, _ := user32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&class)))
		alignmentClassOK = atom != 0
		if !alignmentClassOK && brush != 0 {
			win.NewLazySystemDLL("gdi32.dll").NewProc("DeleteObject").Call(brush)
		}
	})
	if !alignmentClassOK {
		return 0
	}
	exstyle := uintptr(0x80000 | 0x20 | 0x8000000 | 0x80) // layered, transparent, no activate, tool window.
	style := uintptr(0x40000000)                          // WS_CHILD keeps normal guides on the desktop.
	if foreground {
		exstyle |= 8 // WS_EX_TOPMOST only during Win+A.
		style = 0x80000000
		parent = 0
	}
	hwnd, _, _ := user32.NewProc("CreateWindowExW").Call(exstyle, uintptr(unsafe.Pointer(alignmentClassName)), 0, style, 0, 0, 1, 1, parent, 0, module, 0)
	if hwnd != 0 {
		_ = setWindowAlpha(hwnd, 235)
	}
	return hwnd
}

func (guides *AlignmentGuides) Clear() {
	guides.hide()
	*guides = AlignmentGuides{}
}

// Hiding a guide must not reset the unconstrained mouse-drag origin.
func (guides *AlignmentGuides) hide() {
	for _, hwnd := range []uintptr{guides.horizontal, guides.vertical} {
		if hwnd != 0 {
			user32.NewProc("DestroyWindow").Call(hwnd)
		}
	}
	guides.horizontal, guides.vertical, guides.parent = 0, 0, 0
	guides.foreground = false
}

func (guides *AlignmentGuides) Begin(hwnd uintptr) {
	guides.Clear()
	bounds, ok := physicalWindowRect(hwnd)
	var cursor nativePoint
	valid, _, _ := user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&cursor)))
	button, _, _ := user32.NewProc("GetAsyncKeyState").Call(1) // VK_LBUTTON
	if ok && valid != 0 {
		guides.moving, guides.start, guides.cursor = hwnd, bounds, cursor
		guides.mouseDrag = button&0x8000 != 0
	}
}

func physicalWindowRect(hwnd uintptr) (nativeRect, bool) {
	var rect nativeRect
	r, _, _ := user32.NewProc("GetWindowRect").Call(peekRoot(hwnd), uintptr(unsafe.Pointer(&rect)))
	return rect, r != 0
}

func nativeWindowVisible(hwnd uintptr) bool {
	r, _, _ := user32.NewProc("IsWindowVisible").Call(peekRoot(hwnd))
	return r != 0
}

func (guides *AlignmentGuides) show(horizontal bool, rect nativeRect, parent uintptr, foreground bool) {
	if guides.parent != parent || guides.foreground != foreground {
		guides.hide()
	}
	guides.parent, guides.foreground = parent, foreground
	handle := &guides.vertical
	if horizontal {
		handle = &guides.horizontal
	}
	if *handle == 0 || !IsNativeWindow(*handle) {
		*handle = createAlignmentGuide(parent, foreground)
	}
	if *handle == 0 {
		return
	}
	position := nativePoint{X: rect.Left, Y: rect.Top}
	after := uintptr(0) // HWND_TOP among desktop siblings, never above applications.
	if foreground {
		after = ^uintptr(0) // HWND_TOPMOST
	} else if r, _, _ := user32.NewProc("ScreenToClient").Call(parent, uintptr(unsafe.Pointer(&position))); r == 0 {
		return
	}
	user32.NewProc("SetWindowPos").Call(*handle, after, uintptr(position.X), uintptr(position.Y), uintptr(max(1, rect.Right-rect.Left)), uintptr(max(1, rect.Bottom-rect.Top)), 0x10|0x40|0x200)
}

// Moving changes only the origin of WM_MOVING's physical screen RECT. It never
// resizes the moving HWND, touches sibling geometry, or converts through DIPs.
func (guides *AlignmentGuides) Moving(hwnd, lparam uintptr, siblings []uintptr, locked bool) {
	proposed := (*nativeRect)(unsafe.Pointer(lparam))
	if locked {
		if bounds, ok := physicalWindowRect(hwnd); ok {
			*proposed = bounds
		}
		guides.Clear()
		return
	}
	root := peekRoot(hwnd)
	foreground := root != hwnd
	parent := DesktopParent(hwnd)
	if !foreground && (!IsDesktopAttached(hwnd) || !IsNativeWindow(parent)) {
		guides.Clear()
		return
	}
	if foreground {
		parent = 0
	}
	if guides.moving != hwnd {
		guides.Begin(hwnd)
	}
	if guides.mouseDrag {
		var cursor nativePoint
		if ok, _, _ := user32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&cursor))); ok != 0 {
			// WM_MOVING may be based on the last snapped RECT. Reconstruct the
			// free position from total pointer travel so small moves can escape.
			width, height := proposed.Right-proposed.Left, proposed.Bottom-proposed.Top
			proposed.Left = guides.start.Left + cursor.X - guides.cursor.X
			proposed.Top = guides.start.Top + cursor.Y - guides.cursor.Y
			proposed.Right, proposed.Bottom = proposed.Left+width, proposed.Top+height
		}
	}
	dpi, _, _ := user32.NewProc("GetDpiForWindow").Call(root)
	if dpi == 0 {
		dpi = 96
	}
	scale := float64(dpi) / 96
	threshold := int32(max(1, math.Round(4*scale)))
	thickness := int32(max(1, math.Round(scale)))
	margin := int32(math.Round(12 * scale))
	xDistance, yDistance := threshold+1, threshold+1
	var xOffset, yOffset int32
	var xSibling, ySibling nativeRect
	var horizontalEdge int32
	for _, sibling := range siblings {
		if !nativeWindowVisible(sibling) {
			continue
		}
		bounds, ok := physicalWindowRect(sibling)
		if !ok {
			continue
		}
		delta := bounds.Left - proposed.Left
		if distance := absCoordinate(delta); distance <= threshold && (distance < xDistance || distance == xDistance && bounds.Left < xSibling.Left) {
			xDistance, xOffset, xSibling = distance, delta, bounds
		}
		for _, own := range []int32{proposed.Top, proposed.Bottom} {
			for _, target := range []int32{bounds.Top, bounds.Bottom} {
				delta = target - own
				if distance := absCoordinate(delta); distance <= threshold && (distance < yDistance || distance == yDistance && target < horizontalEdge) {
					yDistance, yOffset, ySibling, horizontalEdge = distance, delta, bounds, target
				}
			}
		}
	}
	if xDistance > threshold && yDistance > threshold {
		guides.hide()
		return
	}
	if xDistance <= threshold {
		proposed.Left += xOffset
		proposed.Right += xOffset
	}
	if yDistance <= threshold {
		proposed.Top += yOffset
		proposed.Bottom += yOffset
	}
	// Establish the host mode before drawing either guide; a mode change must
	// not destroy the first guide while the second one is being positioned.
	if guides.parent != parent || guides.foreground != foreground {
		guides.hide()
	}
	guides.parent, guides.foreground = parent, foreground
	if yDistance <= threshold {
		guides.show(true, nativeRect{Left: min(proposed.Left, ySibling.Left) - margin, Top: horizontalEdge, Right: max(proposed.Right, ySibling.Right) + margin, Bottom: horizontalEdge + thickness}, parent, foreground)
	} else if guides.horizontal != 0 {
		user32.NewProc("ShowWindow").Call(guides.horizontal, 0)
	}
	if xDistance <= threshold {
		guides.show(false, nativeRect{Left: proposed.Left, Top: min(proposed.Top, xSibling.Top) - margin, Right: proposed.Left + thickness, Bottom: max(proposed.Bottom, xSibling.Bottom) + margin}, parent, foreground)
	} else if guides.vertical != 0 {
		user32.NewProc("ShowWindow").Call(guides.vertical, 0)
	}
}

func absCoordinate(value int32) int32 {
	if value < 0 {
		return -value
	}
	return value
}
