//go:build windows

package shell

import (
	"golang.org/x/sys/windows/registry"
	"os"
	"path/filepath"
)

// HasWebView2 checks evergreen runtime registration/installations, without downloading anything.
func HasWebView2() bool {
	const client = `Software\Microsoft\EdgeUpdate\Clients\{F1E7E5A0-0401-4E28-9933-0A5E32D9CB0F}`
	for _, root := range []registry.Key{registry.CURRENT_USER, registry.LOCAL_MACHINE} {
		for _, view := range []uint32{registry.WOW64_32KEY, registry.WOW64_64KEY} {
			k, e := registry.OpenKey(root, client, registry.QUERY_VALUE|view)
			if e == nil {
				v, _, e := k.GetStringValue("pv")
				k.Close()
				if e == nil && v != "" && v != "0.0.0.0" {
					return true
				}
			}
		}
	}
	for _, base := range []string{os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramFiles"), os.Getenv("LOCALAPPDATA")} {
		matches, _ := filepath.Glob(filepath.Join(base, "Microsoft", "EdgeWebView", "Application", "*", "msedgewebview2.exe"))
		if len(matches) > 0 {
			return true
		}
	}
	return false
}
