//go:build windows

package app

import (
	"math"
	"time"

	native "deskgrid/internal/windows"
	"github.com/wailsapp/wails/v3/pkg/application"
)

type windowSizeAnimation struct {
	fromWidth, fromHeight int
	width, height         int
}

// Only the bottom edge moves. Keep the real HWND and its rounded region in
// sync so the transition also works while hosted above the desktop by Win+A.
func (h *Desktop) animateWindowSize(id string, win *application.WebviewWindow, animation *windowSizeAnimation) {
	const duration = 180 * time.Millisecond
	started := time.Now()
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
		}
		progress := min(1, float64(time.Since(started))/float64(duration))
		eased := progress * progress * (3 - 2*progress)
		width := animation.fromWidth + int(math.Round(float64(animation.width-animation.fromWidth)*eased))
		height := animation.fromHeight + int(math.Round(float64(animation.height-animation.fromHeight)*eased))
		var frameErr error
		active := false
		finished := progress >= 1
		application.InvokeSync(func() {
			h.mu.Lock()
			state := h.applied[id]
			current := h.main[id] == win && state.sizeAnimation == animation && !h.quitting
			pixelRatio := h.pixelRatios[id]
			h.mu.Unlock()
			hwnd := uintptr(win.NativeWindow())
			if !current || !native.IsNativeWindow(hwnd) {
				return
			}
			active = true
			frameErr = native.ResizeDesktopWindow(hwnd, width, height)
			if frameErr != nil {
				// Try the final dimensions once, then restore normal resize controls.
				finished = true
				_ = native.ResizeDesktopWindow(hwnd, animation.width, animation.height)
			}
			if finished {
				state.sizeAnimation = nil
				h.mu.Lock()
				h.applied[id] = state
				h.mu.Unlock()
				if !state.collapsed {
					win.SetMinSize(220, 120)
				}
				win.SetResizable(!state.positionLocked)
			}
			// The current HWND height is the clipping edge during every frame;
			// applying the final collapsed height early would hide the animation.
			if err := native.RoundWindow(hwnd, 0, pixelRatio, state.cornerRadius); frameErr == nil {
				frameErr = err
			}
		})
		if !active {
			return // Closed, reopened, or superseded by a newer transition.
		}
		if frameErr != nil {
			h.notifyError(frameErr)
		}
		if finished {
			h.scheduleGeometry(id)
			go application.InvokeAsync(h.correctAll)
			return
		}
	}
}
