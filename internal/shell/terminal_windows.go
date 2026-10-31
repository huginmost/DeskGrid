//go:build windows

package shell

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf16"
	"unsafe"

	"golang.org/x/sys/windows"
)

func openTerminal(dir, kind string) error {
	systemDir, err := windows.GetSystemDirectory()
	if err != nil {
		return fmt.Errorf("读取终端程序目录: %w", err)
	}
	program := filepath.Join(systemDir, "cmd.exe")
	arguments := "/D /V:OFF /K"
	cwd := dir
	unc := strings.HasPrefix(filepath.VolumeName(dir), `\\`)
	if kind == "cmd" {
		// A local path is passed through lpCurrentDirectory, never through CMD
		// parsing. UNC requires pushd because CMD cannot use it as its cwd.
		if unc {
			if strings.ContainsAny(dir, "%\r\n\"!") {
				return fmt.Errorf("cmd 无法安全解析此网络路径；请选择 PowerShell")
			}
			cwd = systemDir
			arguments += ` pushd "` + dir + `"`
		}
	} else {
		program = filepath.Join(systemDir, "WindowsPowerShell", "v1.0", "powershell.exe")
		literal := "'" + strings.ReplaceAll(dir, "'", "''") + "'"
		units := utf16.Encode([]rune("Set-Location -LiteralPath " + literal))
		encoded := make([]byte, len(units)*2)
		for i, unit := range units {
			binary.LittleEndian.PutUint16(encoded[i*2:], unit)
		}
		arguments = "-NoLogo -NoExit -EncodedCommand " + base64.StdEncoding.EncodeToString(encoded)
		if unc {
			cwd = systemDir
		}
	}
	applicationName, err := windows.UTF16PtrFromString(program)
	if err != nil {
		return err
	}
	commandLine, err := windows.UTF16FromString(windows.EscapeArg(program) + " " + arguments)
	if err != nil {
		return err
	}
	workingDirectory, err := windows.UTF16PtrFromString(cwd)
	if err != nil {
		return err
	}
	// os/exec supplies NUL for nil stdin and sets STARTF_USESTDHANDLES. That
	// makes an interactive shell immediately encounter EOF. Let the new console
	// initialise its own stdin/stdout/stderr instead; inherit no application handles.
	startup := windows.StartupInfo{Flags: windows.STARTF_USESHOWWINDOW, ShowWindow: windows.SW_SHOWNORMAL}
	startup.Cb = uint32(unsafe.Sizeof(startup))
	var process windows.ProcessInformation
	if err = windows.CreateProcess(applicationName, &commandLine[0], nil, nil, false, windows.CREATE_NEW_CONSOLE, nil, workingDirectory, &startup, &process); err != nil {
		return fmt.Errorf("在 %s 中打开 %s: %w", dir, kind, err)
	}
	// Closing our kernel handles releases resources without ending the shell.
	windows.CloseHandle(process.Thread)
	windows.CloseHandle(process.Process)
	return nil
}
