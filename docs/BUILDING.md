# DeskGrid 源码与构建

DeskGrid 使用 Go + Wails v3 + Vue 3 + TypeScript，目标为 Windows amd64。前端资源在生产构建时内嵌到 EXE 中。

## 环境

- Windows 10/11 x64。
- Go 1.27.3（go.mod 要求 1.27.0 及以上）；可使用已安装工具链，或执行 `scripts/bootstrap-go.ps1` 下载到本项目的 `.tools`。
- Node.js 22.12+、npm；当前构建使用 Node.js 24.20.0 / npm 11.19.0。
- Microsoft Edge WebView2 Runtime，用于运行桌面界面。

Go 和前端依赖分别由 `go.mod` / `go.sum`、`frontend/package-lock.json` 锁定。首次安装依赖需要联网。

## 构建

在项目根目录的 PowerShell 中执行：

```powershell
# 已安装 Go 时可省略此步
.\scripts\bootstrap-go.ps1

# 安装前端依赖、编译前端与 Windows EXE、收集许可证
.\scripts\build.ps1 -SkipTests

# 已有 frontend/node_modules 时复用依赖
.\scripts\build.ps1 -SkipInstall -SkipTests
```

程序输出为 `bin\DeskGrid.exe`。正常构建不运行测试，也不启动程序；已有测试源码保留在仓库内。

Windows 图标与产品属性由 `scripts/windows-resources` 使用 Go 标准库生成。可编辑素材在 `internal/branding`，无需额外图像工具即可构建；仅重新生成素材时需要 Python + Pillow，执行 `python scripts/generate-icons.py`。

## 发布目录

```powershell
.\scripts\package.ps1
```

脚本从 `bin\DeskGrid.exe` 打包，生成：

```text
release/
  README.md                     发布操作说明（入库）
  DeskGrid/                     解压版目录，含 EXE、说明、许可证与安装脚本
  DeskGrid-windows-amd64.zip     发布压缩包
  SHA256SUMS.txt                 压缩包和 EXE 的 SHA256
  RELEASE_NOTES.md               发布说明
```

发布产物、工具缓存、node_modules、前端构建目录不提交 Git。发布 ZIP 和校验文件应作为 GitHub Release 的附件上传。仅生成本地发布目录不会创建 GitHub Release 或标签。

## 安装脚本

`scripts/install.ps1` 可从源码目录或解压目录执行，默认安装到 `%LOCALAPPDATA%\Programs\DeskGrid`，建立开始菜单与当前用户卸载项。自启和资源管理器集成在软件设置中按需启用。

卸载时运行安装目录中的 `uninstall.ps1`，默认保留配置；显式 `-RemoveConfig` 才删除当前默认配置目录。

## 源码结构

| 目录 | 职责 |
| --- | --- |
| `internal/app` | 组合服务、窗口、桌面挂载、动画和交互 |
| `internal/windows` | Windows 桌面层、菜单、快捷键、IPC、吸附参考线 |
| `internal/shell` | Shell、剪贴板、图标提取、终端和系统集成 |
| `internal/filesystem` | 文件操作、目录监听与移动撤销 |
| `internal/config`、`internal/model` | JSON 配置与数据模型 |
| `internal/appearance`、`internal/commands`、`internal/shortcuts` | 外观、菜单命令与虚拟入口 |
| `internal/branding` | DeskGrid 原创图标 |
| `frontend/src` | Vue 界面 |
| `scripts` | 构建、图标资源、安装、卸载与打包 |

配置默认位于 `%APPDATA%\DeskGrid`，已有旧版配置时兼容 `%APPDATA%\sticker`。开发时可用 `--config <绝对目录>` 指定独立配置；不要把用户配置、WebView 缓存或日志提交到仓库。
