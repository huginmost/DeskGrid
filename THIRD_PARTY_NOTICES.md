# Third-party notices

- Wails v3.0.0 — MIT — https://github.com/wailsapp/wails ; includes its pinned WebView2 loader/runtime and transitive dependencies under their original licenses.
- Vue 3.5.22 — MIT — https://github.com/vuejs/core
- Lucide icons / lucide-vue-next 0.468.0 — ISC — https://github.com/lucide-icons/lucide
- fsnotify 1.9.0 — BSD-3-Clause — https://github.com/fsnotify/fsnotify
- golang.org/x/sys 0.48.0 — BSD-3-Clause — https://go.googlesource.com/sys
- go-ole 1.3.0 — MIT — https://github.com/go-ole/go-ole
- xdg 0.5.3 — MIT — https://github.com/adrg/xdg
- go-colorable 0.1.14 / go-isatty 0.0.20 — MIT — https://github.com/mattn
- TypeScript, Vite, Vitest, Vue Test Utils and jsdom are development dependencies; their exact versions and upstream license metadata remain in package-lock.json and installed package LICENSE files.

Windows application/file icons are extracted at runtime from the user's local Shell and are not redistributed as an icon theme. System fonts are requested by family and not bundled. DeskGrid's application and theme-aware tray icons are original geometric artwork; their editable SVG sources and generated PNG/ICO assets are in internal/branding. scripts/generate-icons.py uses Pillow only to regenerate artwork; Pillow is not bundled or needed by normal builds. Supplied image*.png are user-provided visual references and are not embedded into the running program. No downloaded web images, fonts or remote scripts are used.

The distribution build copies the complete license texts from the pinned local dependency trees into bin/licenses; keep that folder with redistributed binaries.
