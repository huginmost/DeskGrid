# Core implementation verification

Implemented schema version 1 protocol model types, field-level appearance inheritance, serialized configuration writes and virtual entry operations, and structured custom command parsing and menu inheritance.

Configuration writes use a same-directory temporary file, Flush/Sync and Windows MoveFileExW with replacement/write-through flags (os.Rename on other platforms). Existing valid configurations are copied to backups before replacement. Invalid files are copied to timestamped `.corrupt-*` files; valid backups are recovered with visible warnings. Duplicate IDs use the highest valid revision. Unknown future schemas remain protected, including attempts to reuse their window IDs under different titles. Window identity/type/root and the start definition are protected.

Menu global keys are `<window-type>:<context>`; window keys are contexts, with the compound key also accepted. Appearance overrides and menu action patches use pointers for per-field inheritance. Menu custom entries require an explicit custom flag plus name/template. Unknown historical action IDs do not become executable entries.

Virtual operations clone before mutation; an error leaves both windows unchanged. Deduplication compares canonical absolute paths and existing file identity, retaining platform case semantics. Groups are one layer; empty groups disappear and one-member groups remain. Dissolving a group which would introduce duplicate root targets returns an error without changes.

Commands parse Windows quoting/backslashes and empty arguments before substitution. `%paths%` expands into separate arguments; single-target templates execute once per selected path. Explicit shell modes return structured commands without execution. Shell substitutions must occupy complete arguments. cmd substitutions containing quotes, percent signs, NUL/newlines are rejected with program/PowerShell fallback guidance; PowerShell values use literal single-quote escaping. Program mode rejects shell syntax and batch programs. These restrictions are intentional rather than unsafe interpolation.

Automated verification was executed on Windows using Go 1.27 with local TMP/TEMP under `.tools/tmp`. Suites cover configuration corruption/recovery/future schemas/protected identities/deep copies, appearance validation/inheritance/nonfinite values, Windows command parsing/variables/menu inheritance, and transactional virtual grouping/transfers with unchanged disk targets. Tests first reproduced missing implementations and later concrete corruption, shell-substitution, ungroup duplicate and nonfinite appearance defects before fixes.

No native Shell or command execution interactions were exercised by these packages' tests. Native OS integration and application-level acceptance are outside these package tests. No earlier schema migration exists because version 1 is the initial format; all unsupported schemas produce warnings and are not silently migrated.
