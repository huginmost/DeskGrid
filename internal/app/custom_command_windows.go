//go:build windows

package app

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func newCustomProcess(program string, args []string, directory, mode string) (*exec.Cmd, error) {
	command := exec.Command(program, args...)
	command.Dir = directory
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	switch mode {
	case "cmd":
		if len(args) != 5 || args[3] != "/C" {
			return nil, fmt.Errorf("CMD 命令格式无效")
		}
		// CMD does not use CommandLineToArgvW. Preserve the shell expression
		// verbatim inside /S /C's outer quotes; Go's default quoting adds literal
		// backslashes before its quotes and breaks commands such as code %path%.
		command.SysProcAttr.CmdLine = windows.EscapeArg(program) + " /D /V:OFF /S /C \"" + args[4] + "\""
	case "powershell":
		if len(args) != 4 || args[2] != "-Command" {
			return nil, fmt.Errorf("PowerShell 命令格式无效")
		}
		units := utf16.Encode([]rune(args[3]))
		encoded := make([]byte, len(units)*2)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(encoded[i*2:], unit)
		}
		command.Args = []string{program, args[0], args[1], "-EncodedCommand", base64.StdEncoding.EncodeToString(encoded)}
	}
	// A launcher may exit while its GUI child still owns an output handle.
	// Do not keep a reader goroutine alive until that child eventually exits.
	command.WaitDelay = time.Second
	return command, nil
}

// Retain a bounded diagnostic tail even for commands that produce continuous
// output. stdout and stderr may write concurrently.
type commandOutput struct {
	mu   sync.Mutex
	data []byte
}

func (b *commandOutput) Write(p []byte) (int, error) {
	const limit = 8192
	n := len(p)
	b.mu.Lock()
	defer b.mu.Unlock()
	if n >= limit {
		b.data = append(b.data[:0], p[n-limit:]...)
	} else {
		if extra := len(b.data) + n - limit; extra > 0 {
			copy(b.data, b.data[extra:])
			b.data = b.data[:len(b.data)-extra]
		}
		b.data = append(b.data, p...)
	}
	return n, nil
}

func (b *commandOutput) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.TrimSpace(strings.ToValidUTF8(string(b.data), "�"))
}
