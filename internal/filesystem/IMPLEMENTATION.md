# Filesystem implementation

## Public interface

- `Entry`, `Listing`, `Result`, `Task` use the JSON fields in `docs/protocol.md`.
- `List(path string, showHidden bool, offset, limit int) (Listing, error)` reads only one directory, sorts directories first then natural case-insensitive names, paginates after filtering, refuses direct reparse-point expansion. `limit <= 0` means all remaining entries.
- `Stat(paths []string) []Entry` returns per-path errors without blocking other results.
- `Within(root, path string) bool` is lexical root-boundary validation. `Confined(root, path string) error` additionally resolves existing paths and checks real root bounds; use the latter before root-bound filesystem access/navigation.
- `ValidateName(name string) error`, `Rename(root,path,name string) error`, `Create(root,parent,name string,directory bool) error` validate Windows names and confined parents; exclusive creation/no-overwrite rename.
- `NewManager() *Manager`; `Start(paths []string,dest,operation,conflict string) (Task,error)`; `Tasks() []Task`; `Cancel(id string) error`; `Resolve(id,choice string,applyAll bool) error`; `Active() bool`.
- `Watch(paths []string, notify func()) (cancel func(), err error)` uses fsnotify for exactly supplied loaded directories; callback runs on watcher goroutine and should return promptly. Caller owns replacing/cancelling watchers when loaded directory set changes.

## Task behavior

Start validates all top-level sources, source overlap, same/descendant targets, operation, destination and policy before spawning a worker. Source links/reparse points are rejected explicitly. Nested reparse points are never traversed; copy reports a failure if encountered. A same-volume move can rename a whole directory containing links without following them.

Directory conflicts merge. File conflicts support ask/skip/replace/keepBoth; pending conflict source/target appears in results with status conflict. Resolve can apply a policy to subsequent conflicts. Cancel also wakes a pending ask. Results retain historical conflict rows alongside final outcome rows; consumers should display the latest outcome per source/target if desired. Completed/Total count top-level submitted sources; Results include successfully processed descendants and per-entry failures. Bytes counts actual written copy bytes; rename moves write no copy bytes.

Files copy into unique staging paths in the destination directory. Windows uses CopyFileExW with FAIL_IF_EXISTS, preserving native streams and metadata, with real byte progress and cancellation callbacks. Commit uses exclusive MoveFile for new targets or MoveFileExW replacement for existing targets; old destination is preserved if staging/cancellation fails. Source ADS enumeration and target FILE_NAMED_STREAMS validation reject destinations that cannot preserve source streams. Partially written new targets and staging files are removed on errors/cancel. Cross-volume move removes each source only after committed copy; failed/skipped sources stay. Existing destination directory contents are never removed wholesale. Cancellation retains already completed items, and partial failure is not a rollback.

No recycle/permanent-delete API is provided. Native Shell recycle is owned by integration.

## Verification

`.tools/go/bin/go.exe test ./internal/filesystem`

Real `t.TempDir` tests cover natural ordering/pagination, reserved/invalid names, root prefix escapes, same/descendant transfer rejection, merged directory preservation, skip/cancel source preservation, keep-both, conflict resolve/replacement, partial move failure, reparse confinement (skips if Windows symlink privilege unavailable), and directory notifications.

The initial suite passed. Follow-up tests caught progress double-counting and native Windows replacement limitations; both corrected. Native replacement test passes outside sandbox; sandbox Windows replacement returns Access denied. Run complete suite outside sandbox for this native API check.

## Limits requiring accurate UI/product reporting

- Reparse-point transfer is explicitly unsupported rather than followed/copied as its target. No complete reparse data cloning.
- No icon extraction here; integration supplies icon cache.
- Destination/source symlink changes performed concurrently by another process between path checks and OS calls are not secured with handle-relative NT APIs. Confinement validates existing ancestors but is not an adversarial filesystem security boundary. Existing target file replacement also has this external mutation race; task workers themselves never delete target directories.
- Watcher errors trigger refresh notification; no filesystem polling fallback is installed.
- Top-level task history remains in manager memory for process lifetime.
- ReadDir loads names of one directory before sorting; large directories remain asynchronous through UI RPC integration, with pagination reducing rendered output rather than directory enumeration cost.
- Windows file copying delegates stream and metadata preservation to CopyFileExW. ADS copy and move-replacement preservation are verified on NTFS. No non-NTFS destination is available in this environment, so rejection there is implemented using native volume flags but not physically tested. Full ACL/encryption/sparse metadata matrices are not tested. Directory creation/merge does not clone source directory metadata. Non-Windows fallback copies regular file data only. Devices/special entries are not supported transfer sources. Native copy progress currently requires a 64-bit Windows build.
- Native long-path behavior depends on Go/Windows process policy; no shell path interpolation occurs.

## ADS data-loss review fix

A real source plus :secret stream regression failed with the previous chunked default-stream copy (missing stream at target). After native CopyFileExW implementation, copy and move-replace both preserve the stream. Native cancellation test cancels from a real byte-progress callback, verifies source size survives and partial target is absent. Exclusive-target test verifies existing destination data survives failed native copy.

Final command (outside sandbox): .tools/go/bin/go.exe test ./internal/filesystem -count=1 -v. Result: 12 passed; one symlink test skipped for unavailable Windows privilege. Cross-volume hardware transfer and target-volume stream rejection were not physically tested.

