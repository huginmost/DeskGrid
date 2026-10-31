//go:build windows

package main

import (
	"deskgrid/internal/app"
	"deskgrid/internal/config"
	"deskgrid/internal/shell"
	native "deskgrid/internal/windows"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"
)

func main() {
	configFlag := flag.String("config", "", "配置目录；默认 %APPDATA%\\DeskGrid，兼容旧版配置")
	pin := flag.Bool("pin", false, "固定后续真实路径到所选应用窗口")
	smoke := flag.Bool("smoke", false, "新建第二个独立测试窗口")
	quitAfter := flag.Duration("quit-after", 0, "仅用于测试，指定时间后退出")
	unregister := flag.Bool("unregister", false, "清理当前用户自启和资源管理器集成后退出")
	quit := flag.Bool("quit", false, "请求已有实例安全退出")
	settings := flag.Bool("settings", false, "打开设置")
	folder := flag.String("folder", "", "打开绑定到指定目录的窗口")
	flag.Parse()
	if *unregister {
		e := shell.SetIntegration(false, "")
		if e == nil {
			e = shell.SetAutoStart(false, "")
		}
		if e != nil {
			shell.Message("DeskGrid 卸载清理失败", e.Error())
			os.Exit(1)
		}
		return
	}
	dir := *configFlag
	if dir == "" {
		base := os.Getenv("APPDATA")
		if base == "" {
			shell.Message("DeskGrid", "无法确定 APPDATA 配置位置")
			os.Exit(1)
		}
		var err error
		dir, err = config.DefaultDirectory(base)
		if err != nil {
			shell.Message("DeskGrid 配置加载失败", err.Error())
			os.Exit(1)
		}
	}
	dir, e := filepath.Abs(dir)
	if e != nil {
		panic(e)
	}
	requests := make(chan native.Request, 128)
	req := native.Request{Quit: *quit, Settings: *settings, Folder: *folder}
	if *pin {
		req.Paths = flag.Args()
	}
	instance, primary, e := native.Acquire(dir, req, func(r native.Request) { requests <- r })
	if e != nil {
		shell.Message("DeskGrid 启动失败", e.Error())
		os.Exit(1)
	}
	if !primary {
		return
	}
	defer instance.Close()
	if *quit {
		return
	}
	if e = os.MkdirAll(filepath.Join(dir, "logs"), 0700); e != nil {
		shell.Message("配置不可写", e.Error())
		os.Exit(1)
	}
	logFile, e := os.OpenFile(filepath.Join(dir, "logs", "deskgrid.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e == nil {
		defer logFile.Close()
		slog.SetDefault(slog.New(slog.NewTextHandler(logFile, nil)))
	}
	if !shell.HasWebView2() {
		shell.Message("需要 Microsoft Edge WebView2", "DeskGrid 需要 WebView2 Evergreen Runtime。请从 https://developer.microsoft.com/microsoft-edge/webview2/ 安装对应 Windows 64 位运行时，然后重新启动 DeskGrid。你的配置不会被清除。")
		os.Exit(1)
	}
	store, e := config.Open(dir)
	if e != nil {
		shell.Message("DeskGrid 配置加载失败", e.Error())
		os.Exit(1)
	}
	global, _, _, _ := store.Snapshot()
	if exe, err := os.Executable(); err == nil {
		if err := shell.MigrateLegacyRegistration(global.AutoStart, global.ShellIntegration, exe); err != nil {
			slog.Warn("更新旧版系统集成失败", "error", err)
		}
	}
	desktop := app.NewDesktop(store, dir)
	if e = desktop.Restore(); e != nil {
		shell.Message("DeskGrid 恢复失败", e.Error())
		os.Exit(1)
	}
	handle := func(r native.Request) {
		if r.Quit {
			if e := desktop.Quit(false); e != nil {
				slog.Error("退出被任务阻止", "error", e)
			}
			return
		}
		desktop.Activate(r.Paths)
		if r.Settings {
			desktop.Settings("", "")
		}
		if r.Folder != "" {
			_, e := desktop.Service.Call("CreateWindow", jsonBytes(map[string]any{"type": "folder", "path": r.Folder}))
			if e != nil {
				slog.Error("打开目录", "error", e)
			}
		}
	}
	go func() {
		for r := range requests {
			handle(r)
		}
	}()
	if *pin || *settings || *folder != "" {
		go func() { time.Sleep(time.Second); handle(req) }()
	}
	if *smoke {
		go func() {
			time.Sleep(2 * time.Second)
			_, e := desktop.Service.Call("CreateWindow", []byte(`{"type":"collection","title":"常用工具"}`))
			if e != nil {
				slog.Error("smoke", "error", e)
			}
		}()
	}
	if *quitAfter > 0 {
		go func() {
			time.Sleep(*quitAfter)
			if e := desktop.Quit(false); e != nil {
				slog.Error("quit-after", "error", e)
			}
		}()
	}
	if e = desktop.App.Run(); e != nil {
		slog.Error("run", "error", e)
		fmt.Fprintln(os.Stderr, e)
		shell.Message("DeskGrid 运行失败", e.Error())
		os.Exit(1)
	}
}
func jsonBytes(v any) []byte { b, _ := json.Marshal(v); return b }
