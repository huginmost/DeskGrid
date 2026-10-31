# Local UI / Go contract

`POST /api/rpc` JSON `{ "method": "Snapshot", "params": { ... } }` → `{ "result": ..., "error": "" }`. Errors have nonempty `error`; never infer success from HTTP alone. Only embedded frontend has access; service has an explicit method allowlist. Paths remain JSON strings, never shell-interpolated commands.

Each native window URL is `/?window=<uuid>`. Settings use `/?settings=<uuid>` or `/?settings=global`. Pin chooser uses `/?pin=1`. The on-demand file task window uses `/?tasks=1` (native window ID `tasks`).

Types (camelCase JSON):

```ts
type Entry={id:string,type:'path'|'group',path?:string,displayName?:string,name?:string,items?:Entry[]};
type Appearance={opacity:number,scale:number,titleColor:string,titleTextColor:string,backgroundColor:string,folderColor:string,textColor:string};
type Bounds={x:number,y:number,width:number,height:number,unit:'dip',displayId:string};
type WindowDef={schemaVersion:number,id:string,revision:number,type:'start'|'collection'|'folder',title:string,titleMode:'auto'|'custom',layout:'grid'|'list',bounds:Bounds,expandedSize:{width:number,height:number},collapsed:boolean,collapsedHeight?:number,positionLocked:boolean,items:Entry[],rootPath:string,currentPath:string,expandedPaths:string[],appearanceOverrides:Partial<Appearance>,menuOverrides:Record<string,MenuPatch>};
type FileEntry={path:string,name:string,isDir:boolean,isLink:boolean,hidden:boolean,size:number,modified:string,icon?:string,error?:string};
type Drive={path:string,name:string,total:number,free:number,error?:string};
type MenuAction={id:string,name:string,enabled:boolean,group?:string,custom?:boolean,mode?:'program'|'cmd'|'powershell',template?:string};
type MenuPatch={order?:string[],actions?:Record<string,Partial<MenuAction>>};
type Global={schemaVersion:number,theme:'system'|'light'|'dark',appearance:Partial<Appearance>,menus:Record<string,MenuPatch>,hiddenFiles:boolean,reducedMotion:boolean,desktopMode:boolean,autoStart:boolean,shellIntegration:boolean,hotkey:string,peekHotkey?:string};
type Task={id:string,operation:string,status:string,total:number,completed:number,bytes:number,results:{source:string,target:string,status:string,error?:string}[],error?:string};
type Snapshot={window?:WindowDef,windows:WindowDef[],openWindowIds:string[],global:Global,appearance:Appearance,drives:Drive[],warnings:string[],tasks:Task[],status:{desktop:string,hotkey:string,peekHotkey?:string,autoStart:string,shellIntegration:string},pendingPins:string[],systemDark:boolean};
```

RPC methods and params:

- `Snapshot {windowId?:string}` → Snapshot
- `CreateWindow {type:'collection'|'folder',path?:string,title?:string}` → WindowDef (also opens native window)
- `OpenWindow {windowId}` / `CloseWindow {windowId}` / `DeleteWindow {windowId}` → void (Close and Delete remove the window definition and JSON, session membership, and matching backups directly; disk targets remain; start and position-locked main windows cannot be closed or deleted)
- `UpdateWindow {windowId,patch:Partial<WindowDef>}` → WindowDef (identity/type/root protected)
- `ListDirectory {windowId,path,offset?:number,limit?:number}` → `{entries:FileEntry[],total:number}`; folder root confinement enforced; start tree permits drive paths
- `StatPaths {paths:string[]}` → FileEntry[]; UI can mark missing pinned entries
- `Navigate {windowId,path}` → WindowDef
- `Pin {windowId,paths:string[],groupId?:string,targetEntryId?:string}` → `{window:WindowDef,duplicates:string[]}`; targetEntryId groups newly pinned entries with that existing top-level path entry, preserving duplicate reporting. It is mutually exclusive with groupId.
- `MutateEntries {windowId,operation:'remove'|'rename'|'group'|'ungroup'|'reorder'|'moveToGroup',ids:string[],name?:string,groupId?:string,index?:number}` → WindowDef
- `TransferEntries {sourceWindowId,targetWindowId,ids:string[],copy:boolean,groupId?:string,index?:number,targetEntryId?:string}` → void; targetEntryId groups transferred entries with that existing top-level path entry, and cannot be combined with groupId. Transfer and group validation finish on cloned models before either window is saved; destination still saves first for crash safety.
- `OpenPath {path,verb?:'open'|'runas'|'openas'|'reveal'|'cmd'|'powershell'}` → void; cmd/powershell resolve the directory (or a file's parent) and use CreateProcessW with CREATE_NEW_CONSOLE, without STARTF_USESTDHANDLES or application handle inheritance. The new console provides interactive stdin/stdout/stderr. System executables use absolute System32 paths; CMD keeps /K and uses native cwd for local directories or safely quoted pushd for UNC. PowerShell keeps -NoExit and receives UTF-16LE EncodedCommand with a single-quoted LiteralPath. Parent process/thread handles close immediately without terminating the terminal.
- `ChooseFolder {}` → string (native folder picker)
- `ClipboardWrite {paths:string[],cut:boolean}` / `ClipboardText {text:string}` → void
- `Paste {windowId,path?:string,conflict?:'ask'|'skip'|'replace'|'keepBoth'}` → Task | WindowDef
- `TransferFiles {windowId:string,paths:string[],destination:string,operation:'copy'|'move',conflict:'ask'|'skip'|'replace'|'keepBoth'}` → Task; destination must resolve within that real folder window's root
- `Recycle {paths:string[]}` → Task; no permanent fallback
- `RenameFile {windowId,path,name}` / `CreateFile {windowId,path,name,directory:boolean}` → void
- `CancelTask {taskId}` / `ResolveConflict {taskId,choice:'skip'|'replace'|'keepBoth'|'cancel',applyAll:boolean}` → void
- `SaveGlobal {patch:Partial<Global>}` → Global
- `ApplyAppearanceGlobal {windowId}` / `ResetAppearance {windowId}` → void
- `GetMenu {windowId,context:'blank'|'folder'|'file'|'group',global?:boolean}` → MenuAction[]; global mode omits window overrides
- `SaveMenu {windowId,context,patch:MenuPatch,global:boolean}` / `ResetMenu {windowId,context,global:boolean}` → void
- `PreviewCommand {template,mode,paths,root}` → `{program:string,args:string[],directory:string}[]`
- `RunCommand {windowId,context,actionId,paths}` → void
- `Settings {windowId?:string,context?:string}` / `ToggleAll {}` / `Quit {force?:boolean}` → void
- `TogglePeek {}` → void; asynchronously toggle temporary foreground display of all ready main windows. A second toggle, an outside click or group focus loss fades the group back to its desktop parents and prior global visibility. Internal clicks and focus changes between main windows do not dismiss it. Reduced-motion settings suppress the transition.
- `BeginPeekRecording {windowId:'settings-<id|global>',requestId:string}` / `EndPeekRecording {windowId,requestId}` → void. Capture only while that native settings window owns foreground focus. The recorded chord returns to its view via deskgrid:peek-recorded with {requestId,binding?:string,error?:string,cancelled?:boolean}. Escape without modifiers cancels; session IDs reject stale events/cancellation, including when another settings window begins recording. The configured peek action is suppressed while recording.
- `FileTasks {}` → void; open or focus the independent file task window
- `ReportError {text:string}` → void; record the error and latest status without opening a system dialog; user cancellation is ignored
- `WindowPainted {windowId:string,pixelRatio:number}` → void; release the initially hidden window after frontend layout/fonts/paint preparation, or update its CSS-to-physical-pixel ratio after a DPI change
- `WindowInteracted {windowId:string}` → void; raise a mounted desktop child among its siblings and dismiss other main windows' context menus. During an explicitly invoked peek, raise the temporarily promoted panel within the foreground group.
- `RetryDesktop {}` → void; retries desktop attachment without creating ordinary top-level main windows
- `FinishPin {windowId}` → void (pins pending Explorer paths)
- `BeginDrag {windowId,paths:string[],virtual:boolean}` → void or explicit error; virtual `.lnk` drag is currently disabled pending asynchronous Shell lifetime acceptance
- `NativeDragStart {windowId}` → void (only after >5 DIP threshold, checks position lock)

Native external drop event: `deskgrid:drop` carries `{windowId,paths:string[],ctrl:boolean,shift:boolean,x:number,y:number}`. UI resolves hovered directory at x/y and applies the specified real/virtual semantics. Backend will not use HTML input/upload/download as native drag substitutes. UI may use HTML drag data only for internal entry organisation, including cross-window `application/x-deskgrid` with window ID/entry IDs.

Drop target attributes include data-entry-id for pinned entries and data-group-id for an open group panel, alongside data-directory for real folders. Virtual drops onto ordinary entries create a group immediately, without a centre-hover timer; drops onto groups or their expanded list members join the existing group. Application drag overlays, target highlights and group previews are removed. Real directory operations retain their disk semantics and hover expansion.

Virtual removal calls MutateEntries/remove directly without a confirmation dialog; recycle confirmations for real files remain. Group thumbnails render the first four members through the same EntryIcon component as normal/group-panel items, with shared icon failure state. Clicking the open group's title focuses/selects its inline name field; Enter or blur saves, Escape cancels, and an empty name cancels the edit. Snapshot updates reconcile the active group and close it if removed.

The main content rows always represent the window's drives, pinned entries and expanded list branches. The group overlay renders a separate groupRows collection. Selection, range selection, select-all and keyboard navigation use activeRows for the currently active view; changing the open group clears stale selection and anchors. Group-item navigation scrolls within the group panel, and opening a group does not replace the main list or change its scroll offset. Switching away from grid layout closes the group overlay.

Native notifications use `deskgrid:changed`; UI additionally polls Snapshot every 1500ms and current expanded directories periodically with stale-response tokens. JSON mutations are optimistic only after Go success; no localStorage configuration.

Menu actions are rendered only in the resolved backend order. Consecutive actions with different group values are separated; inapplicable actions are omitted without losing section boundaries. Explicit saved menu ordering remains supported.

Native focus notifications use deskgrid:focus with a boolean detail. Main windows stay hidden until both desktop attachment and WindowPainted are ready; the native window region uses the reported pixelRatio for rounded corners and collapsed clipping.

Settings target choices are the intersection of windows and openWindowIds, never closed history. Legacy closed definitions are pruned on startup only when a valid previous session was read. Main window defaults are 280 by 300 DIP. titleTextColor independently controls title text and falls back to titleColor for old configuration. WindowPainted releases the shell before directory I/O; subsequent directory updates are silent. Settings and file-task modules load only when used.

- ShowContextMenu {windowId,actions:MenuAction[],x:number,y:number} returns the chosen ID or an empty string; positions use client CSS coordinates and the recorded pixelRatio, and Windows limits the native popup to the monitor work area. Owner-drawn native items use the originating window's resolved background/text colours; the popup HWND uses the same native alpha. Collapsed windows return no selection.
- UndoTransfer {} returns an asynchronous undo Task. The in-memory journal reverses completed move steps after cancelling any still-running original transfer; it never overwrites an existing restore target. It does not restore explicitly overwritten content.
- Prepared main views use an internal prepared-main sequence ID. WindowPainted marks that specific view ready; deskgrid:assign binds a real window ID and a bootstrap Snapshot without navigation. Prepared views are not saved or listed. Assigned views replace their browser history URL with the real window URL.
- Transfers and failures do not automatically call FileTasks. Dragging to a real directory defaults to move, Ctrl selects copy, and drag collision policy is keepBoth.
- Global peekHotkey defaults to "Win+A" when absent; the legacy standalone "Win" value migrates on startup, preserving other custom bindings and explicit empty-string disable. Standalone Win is never intercepted. A low-level keyboard hook matches only the complete configured chord, consumes repeats/matching key-up and masks menu activation only for that chord, preserving the regular Start key and unrelated Win combinations. Settings use native recording rather than a fixed selector. The parser supports Ctrl/Alt/Shift/Win plus letters, digits, F1–F24 and common navigation/OEM keys; F1–F24 can be unmodified. SaveGlobal canonicalises the binding and rejects an equivalent visibility shortcut regardless of modifier ordering. Listener state is reported in status.peekHotkey.
- Peek creates a fresh ownerless top-level HWND with WS_EX_TOPMOST and hosts the existing WS_CHILD content inside it. It does not convert an Explorer desktop child into a top-level window or reload the WebView. Native helpers resolve the temporary host for foreground focus, hit ownership, alpha, dragging and resizing; both content and host receive rounded clipping. The host forwards activation, move, resize, DPI and close handling, retaining start/locked-window close protection. Returning reparents content to the desktop and destroys its host; ordinary desktop retries also clean up a host left waiting for Explorer recovery. Native promotion validates root/owner/topmost state. Focus-loss dismissal is armed only after the group acquires foreground focus.
- Scrollbar visibility uses an activity class on the deskgrid surface. Pointer entry/movement, wheel and keyboard input activate it without requiring window focus, with an 800ms idle timer. Pointer leave, browser blur and deskgrid:focus=false clear it. Content scroll events refresh activity only when focused or after actual pointer entry; persistent CSS hover/focus-within selectors no longer keep the thumb visible.
- Main title actions use titlebar hover alone, independently of document/native focus or focus-within. Hidden actions use opacity, visibility and pointer-events so invisible pin/close buttons cannot receive mouse input or produce tooltips. Existing start/locked-window close restrictions remain.
- Shell icons initialise COM on the extraction thread, render black/white composites with GDI flushes and recover per-pixel coverage, supporting legacy mask icons without relying on undefined bitmap alpha. Empty images return an icon error and are never cached. Icon URLs carry a conversion version to bypass previous transparent PNGs cached by WebView. EntryIcon keeps a default glyph visible until the native image has loaded, and uses the same fallback on failure across all views.
- Common image extensions query SHGetFileInfo with SHGFI_USEFILEATTRIBUTES and cache the resulting association icon per extension. Generic-document associations, failed extraction or blank image icons fall back to SIID_IMAGEFILES through SHGetStockIconInfo. Executables, shortcuts and ICO files retain per-file extraction; other file failures can use their association icon. HTTP icon errors use no-store and include native failure details in the local log. URL version 3 bypasses previous browser cache entries. EntryIcon retries local API failures at most twice and uses an image glyph for image-file placeholders/final fallback rather than a generic document glyph.
