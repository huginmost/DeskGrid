# Frontend implementation

## Runtime

Vue 3 + TypeScript + Vite, exact package versions and npm lockfile. `npm ci`, `npm test`, and `npm run build` operate from this directory. Production output is `dist/` for the root Go embed. `npm run dev` serves source assets and proxies `/api` to port 34115; the standalone Vite page cannot replace the native host. Bootstrap imports the pinned local `/wails/runtime.js` before mounting Vue, installing native drop/ready bridges; a missing runtime produces a visible launch error. There is no browser demonstration database, localStorage persistence, file upload, download substitute, or remote resource dependency.

Every native URL binds its window: `?window=<id>`, `?settings=<id|global>`, or `?pin=1`. UI operations call the real allowlisted `POST /api/rpc` contract. Backend errors remain visible even when HTTP status is 200. Snapshot polling runs every 1500ms and expanded directories refresh every 6s. Native `deskgrid:changed`, `deskgrid:drop`, and `deskgrid:error` DOM events are supported; the host owns Wails event bridging and native window opacity.

## Implemented capabilities

- Compact charcoal surface, 32-DIP title bar, local consistent SVG controls, list/grid views, folder-only list view, title click collapse, persisted position lock, close definition preservation, thresholded native move, independent settings/chooser close IDs.
- Real drive region, async directory arrows, confined folder navigation with back/forward/up, permission/error/empty/loading/retry states, link expansion protection, 500-entry load-more pages and virtual rendering above 150 loaded rows. Refresh preserves loaded extent; stale responses cannot replace newer directory results.
- Offscreen selection, Ctrl+A fetching every page in the current directory and already expanded directories (without scanning collapsed descendants), Ctrl toggling, Shift ranges, arrow navigation, Ctrl+A/C/X/V/D, Delete, F2, Enter, Escape and content zoom shortcuts; text editing is excluded from file shortcuts. Unpin and recycle paths are separate, with confirmations explaining real-file consequences.
- Virtual groups, list expansion and grid popover, inline header rename with Enter save/Escape cancel, create/rename/dissolve/remove, reorder and move into/out of groups; internal `application/x-deskgrid` transfer across windows. Dwell grouping and edge reorder are separate. Backend remains final authority for duplicates and invalid group nesting.
- Native path drops pin into virtual containers; real folders copy/move with modifiers and hovered-directory destination, preferring host `attributes['data-directory']` over final element-relative coordinates. A virtual-to-real Shift move requires explicit confirmation. Whole-group file transfers are refused. The status-bar native drag handle starts `BeginDrag` while the button is held beyond five DIP; unsupported host capabilities show the actual error.
- Context menus choose target/selection correctly, protect system rows, gate single-target actions, dispatch file/Shell/clipboard/pinning/creation/window/group operations, honor configured stable action IDs, enabled state, display names and order. Custom commands run only on explicit user clicks. Missing targets can be relinked with a checked absolute path.
- Independent settings window with Window/Menu/Global tabs, explicit target selector, target-specific effective appearance fetch, per-field inheritance, 300ms debounced validated colors/sliders, preview/reset/apply-global, context-isolated menu editing, sparse field patches, custom command parser preview, global toggles/status/theme/hotkey capture and saved-window reopen.
- Pin chooser consumes pending native Explorer paths. Task panel shows progress/cancel, conflict choices/apply-all, results and errors. Native file icons are lazy local endpoint/data images with bundled generic fallback.

## Verification

- Behavioral tests were written and observed failing before implementing the core selection, safe drop intent, relative-path confinement, stale directory response and HTTP-error behavior.
- Settings tests were observed failing for missing appearance controls and global controls before implementation. Tests additionally protect per-field patch preservation and switching settings targets; a mutation using the previous target's appearance was observed failing the target regression.
- Mounted Vue tests exercise unpin-versus-recycle dispatch, text-edit shortcut isolation, menu override rendering, five-DIP native drag threshold without accidental collapse, real-to-virtual native drop safety, and visible native capability errors. Four additional tests were first observed failing, then passing: inline group Enter rename dispatch without a dialog, Escape cancelling while the group remains open, native host directory attributes taking precedence over conflicting coordinate hits, and scroll-active expiry at 800ms without moving scroll position.
- A paging refresh regression was observed failing at 500 instead of 1000 retained loaded rows before fixing loaded extent preservation.
- `npm test`: 21 tests across three Vitest suites. `node --experimental-strip-types --test tests/core.test.ts`: five additional dependency-free core checks. `npm run build`: vue-tsc and production Vite build.

## Honest limits / remaining native acceptance

Frontend tests use a controlled HTTP boundary; they do not certify Explorer interoperability, COM drag, native clipboard, recycling, desktop mounting, DPI, Explorer restart, shell integration, or OS persistence. Those need native Windows acceptance with the host and real temporary files. No native functionality is simulated in production.

The frontend intentionally pages directory contents for normal navigation. Ctrl+A fetches remaining pages of the current directory and already expanded directories before selecting, including rows outside the viewport, without recursively scanning collapsed descendants. Actual enumeration cost is owned by the filesystem service.

Group header name editing is inline; generic F2 and other rename actions retain the standard dialog. Scroll activity keeps the scrollbar visible for 800ms; hover and focus indicators also remain available. Context menu pin/move destinations are exposed as concrete labelled choices/dialogs rather than nested flyout submenus. Native virtual-entry `.lnk` drag-out is explicitly DISABLED by the host pending verification of asynchronous native drag lifetime. The UI reports that limitation and never moves virtual targets as a substitute. The held-pointer drag handle calls the host for supported real-path sources. Native external-drop hover expansion depends on the host delivering intermediate drag-over events; a final drop event still resolves the correct hovered directory.

Sparse `SaveMenu` patches require the service to merge action fields and preserve omitted order. Global-only menu editing uses `GetMenu {global:true}`. Custom deletion uses the agreed tombstone `{enabled:false,template:''}`. These host contract refinements were coordinated with the root/backend agents.

No commit was made because the root agent requested leaving all changes uncommitted for its integration workflow.
