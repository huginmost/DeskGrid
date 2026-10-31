//go:build windows

package windows

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	win "golang.org/x/sys/windows"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"
)

type Request struct {
	Paths    []string `json:"paths"`
	Quit     bool     `json:"quit,omitempty"`
	Settings bool     `json:"settings,omitempty"`
	Folder   string   `json:"folder,omitempty"`
}
type Instance struct {
	mutex  win.Handle
	pipe   win.Handle
	mu     sync.Mutex
	closed bool
}

func Acquire(key string, request Request, receive func(Request)) (*Instance, bool, error) {
	token := win.GetCurrentProcessToken()
	tu, e := token.GetTokenUser()
	if e != nil {
		return nil, false, e
	}
	var session uint32
	win.NewLazySystemDLL("kernel32.dll").NewProc("ProcessIdToSessionId").Call(uintptr(os.Getpid()), uintptr(unsafe.Pointer(&session)))
	sum := sha256.Sum256([]byte(tu.User.Sid.String() + fmt.Sprint(session) + key))
	id := hex.EncodeToString(sum[:16])
	// Keep the private legacy namespace so an old and renamed executable cannot
	// write the same configuration concurrently. This is not a display name.
	name := `Local\Sticker-` + id
	pipeName := `\\.\pipe\Sticker-` + id
	mutex, e := win.CreateMutex(nil, false, win.StringToUTF16Ptr(name))
	if e != nil && e != win.ERROR_ALREADY_EXISTS {
		return nil, false, e
	}
	if e == win.ERROR_ALREADY_EXISTS {
		if mutex != 0 {
			win.CloseHandle(mutex)
		}
		b, _ := json.Marshal(request)
		b = append(b, '\n')
		var last error
		for i := 0; i < 80; i++ {
			h, e := win.CreateFile(win.StringToUTF16Ptr(pipeName), win.GENERIC_READ|win.GENERIC_WRITE, 0, nil, win.OPEN_EXISTING, 0, 0)
			if e == nil {
				f := os.NewFile(uintptr(h), pipeName)
				_, e = f.Write(b)
				if e == nil {
					_, e = bufio.NewReader(f).ReadString('\n')
				}
				f.Close()
				return nil, false, e
			}
			last = e
			time.Sleep(50 * time.Millisecond)
		}
		return nil, false, fmt.Errorf("已有实例正在启动，转交失败: %w", last)
	}
	// Restrict IPC to the current user and reject remote named-pipe clients.
	sd, e := win.SecurityDescriptorFromString("D:P(A;;GA;;;" + tu.User.Sid.String() + ")")
	if e != nil {
		win.CloseHandle(mutex)
		return nil, false, e
	}
	sa := &win.SecurityAttributes{Length: uint32(unsafe.Sizeof(win.SecurityAttributes{})), SecurityDescriptor: sd}
	h, e := win.CreateNamedPipe(win.StringToUTF16Ptr(pipeName), win.PIPE_ACCESS_DUPLEX|win.FILE_FLAG_FIRST_PIPE_INSTANCE, win.PIPE_TYPE_BYTE|win.PIPE_READMODE_BYTE|win.PIPE_WAIT|win.PIPE_REJECT_REMOTE_CLIENTS, 1, 65536, 65536, 0, sa)
	if e != nil {
		win.CloseHandle(mutex)
		return nil, false, e
	}
	instance := &Instance{mutex: mutex, pipe: h}
	go func() {
		for {
			e := win.ConnectNamedPipe(h, nil)
			if e != nil && e != win.ERROR_PIPE_CONNECTED {
				return
			}
			f := pipeIO{h}
			line, e := bufio.NewReader(io.LimitReader(f, 1024*1024)).ReadString('\n')
			if e == nil && len(line) < 1024*1024 {
				var r Request
				if json.Unmarshal([]byte(line), &r) == nil && len(r.Paths) < 4096 {
					if receive != nil {
						receive(r)
					}
					_, _ = f.Write([]byte("ok\n"))
				}
			}
			win.FlushFileBuffers(h)
			win.DisconnectNamedPipe(h)
			instance.mu.Lock()
			closed := instance.closed
			instance.mu.Unlock()
			if closed {
				return
			}
		}
	}()
	return instance, true, nil
}
func (i *Instance) Close() {
	if i == nil {
		return
	}
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return
	}
	i.closed = true
	win.CancelIoEx(i.pipe, nil)
	win.CloseHandle(i.pipe)
	win.CloseHandle(i.mutex)
}

type pipeIO struct{ h win.Handle }

func (p pipeIO) Read(b []byte) (int, error) {
	var n uint32
	e := win.ReadFile(p.h, b, &n, nil)
	return int(n), e
}
func (p pipeIO) Write(b []byte) (int, error) {
	var n uint32
	e := win.WriteFile(p.h, b, &n, nil)
	return int(n), e
}
