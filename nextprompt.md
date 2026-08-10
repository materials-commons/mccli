I am porting an existing Python Materials Commons CLI project to Go. The Python source lives under
`materials_commons/cli`. The new Go command is `mc2`, with source under `cmd/mc2` and packages under `pkg`.

You are helping me continue this port. Please follow these requirements:

- Be an expert in both Go and Python.
- The Go port should be idiomatic Go, not a line-by-line Python translation.
- Prefer returning `error` over boolean success/failure.
- Use Go best practices such as `errors.Is`, `context.Context`, `slog`, small testable interfaces, and dependency
  injection where useful.
- All generated code should include:
    - file path
    - reasoning for the code
    - documentation comments where appropriate
    - unit tests
- If you find a bug in the Python code while porting, fix it in Go and call it out in the summary.
- Do not add a new external dependency without first listing:
    - dependency name
    - why it is needed
    - why that package was chosen
- Preserve existing user state/layout unless there is a strong reason not to.
- Existing config files are JSON:
    - global config: `$HOME/.materialscommons/config.json`
    - project config: `$PROJECT/.mc/config.json`
- The Go code should continue reading the Python-created state/layout where possible.

Current Go project state:

## CLI

The new command is `mc2`, using `github.com/urfave/cli/v3`.

Subcommands currently laid out:

- `version`
- `clone --id`
- `config [--proj]`
- `down [-r|--recursive] [-f|--force] paths...`
- `init`
- `ls [--action] [paths...]`
- `mkdir [--remote-only] paths...`
- `mv [--remote-only] src target`
- `proj`
- `rm [-r|--recursive] [--remote-only]`
- `remotes [-l|--list] [--show-apikey] [--add remote] [--remove remote] [--set-default url]`
- `up [-r] [--ws-url url] paths...`

The CLI has global logging flags:

- `--log-level`
- `--log-file`

## Build

The `Makefile` builds `bin/mc2` and injects git-derived ldflags:

- `main.version`
- `main.gitTag`
- `main.gitBranch`
- `main.gitCommit`
- `main.gitDate`
- `main.gitDirty`

## Existing Go packages

### `pkg/logging`

Provides context-aware `slog` setup.

- Defaults to stderr.
- Supports configurable level and log file.
- Logger is carried in `context.Context`.

### `pkg/config`

Reads/writes:

- global config at `$HOME/.materialscommons/config.json`
- local project config at `$PROJECT/.mc/config.json`

Global and project config are intentionally separate for now. Project config lookup uses project root discovery.

### `pkg/projectpath`

Centralizes path handling.

Rules:

- local project root maps to remote `/`
- e.g. `/home/user/proj/Aging/Dir/file.txt` maps to `/Dir/file.txt`
- remote `/Dir/file.txt` maps back to local project path
- remote paths are slash paths and always start with `/`
- local paths use `filepath`
- remote paths use `path`

Includes project root discovery by walking upward for `.mc/config.json`.

### `pkg/checksum`

Provides MD5 checksum utilities.

- `MD5File(ctx, path)`
- `MD5FileWithProgress(ctx, path, chunkSize, progress)`

Empty checksum is considered invalid by the reconciler.

### `pkg/filedb`

Project-local SQLite database using GORM generics where appropriate.

DB path: $PROJECT/.mc/mc2.sqlite

Key types:

- `FileRecord`
- `Store`

Important methods:

- `Open(ctx, projectRoot)`
- `OpenPath(ctx, dbPath)`
- `Close(ctx)`
- `Upsert(ctx, record)`
- `UpsertMany(ctx, records)`
- `GetByPath(ctx, remotePath)`
- `ListByDir(ctx, remoteDir)`
- `DeleteByPath(ctx, remotePath)`
- `MarkTransfer(ctx, remotePath, status, origin, transferID)`
- `TouchLocalSeen(ctx, remotePath, seen)`
- `ClearRemoteByPath(ctx, remotePath)`

Important design decisions:

- SQLite uses WAL, busy timeout, synchronous normal.
- `MaxOpenConns(1)` is used initially to avoid SQLite locking issues.
- `Upsert` uses `COALESCE` for nullable fields, so nil incoming values preserve existing values.
- Because `Upsert` cannot clear nullable remote fields, `ClearRemoteByPath` was added.
- `ErrInvalidRecord` validates:
    - paths must start with `/`
    - `Dir` must match `path.Dir(Path)`
    - `Name` must match `path.Base(Path)`
    - checksum pointers must not point to empty strings
- `MarkTransfer`, `TouchLocalSeen`, and `ClearRemoteByPath` return `ErrRecordNotFound` if no row is updated.
- GORM record-not-found logging was configured to avoid noisy expected test output.

### `pkg/reconcile`

This is foundational and has been heavily reviewed.

Core idea:

- The reconciler is a pure decision engine.
- It does not call Materials Commons directly.
- Observation/runner code gathers local, DB, and remote state, then calls the pure reconciler.

Important files:

- `types.go`
- `local.go`
- `observe.go`
- `reconciler.go`
- `walk.go`

#### `types.go`

Important types:

- `Kind`
    - `KindUnknown`
    - `KindFile`
    - `KindDir`
- `Action`
    - `ActionSkip`
    - `ActionUpload`
    - `ActionDownload`
    - `ActionConflict`
    - `ActionDBUpdate`
- `Mode`
    - `ModeUpload`
    - `ModeDownload`
    - `ModeStatus`
    - `ModeSync`
- `LocalEntry`
- `RemoteEntry`
- `Observation`
- `Decision`

Important design:

- `RemoteEntry.RemoteFileID` is `*int64`, not `int64`, because missing remote id must not be confused with id `0`.
- `Observation.LocalEntry == nil` means the path does not exist locally.
- Do not add a synthetic `LocalPath` to `Observation`; use `projectpath.Translator.RemoteToLocal` or
  `WalkNode.LocalPath` when a local destination is needed.

#### `local.go`

`ObserveLocal` converts local filesystem state into `LocalEntry`.

Platform-specific ctime implementations exist:

- `ctime_linux.go`
- `ctime_bsd.go`
- `ctime_fallback.go`

This was done because Linux and BSD/Darwin expose different fields in `syscall.Stat_t`.

#### `observe.go`

Provides the observation runner layer.

Important types/interfaces:

- `FileRecordGetter`
- `RemoteFileGetter`
- `ObservationRunner`
- `FileState`

Important behavior:

- `ObservationRunner.ObserveAndReconcile`:
    1. validates config
    2. translates local path to remote path
    3. observes local filesystem
    4. loads DB record
    5. calls remote `GetFileByPath`
    6. converts remote model to `RemoteEntry`
    7. builds `Observation`
    8. calls pure `Reconciler`

`RemoteFileGetter` is satisfied by `*gomcapi.Client`, whose method is:

```go
GetFileByPath(projectID int, remotePath string) (*mcmodel.File, error)
```

`RemoteEntryFromMCFile` now returns `(*RemoteEntry, error)` and validates:

- remote path is non-empty
- remote path starts with `/`
- name is derived from path if missing
- zero remote id stays nil
- malformed remote data returns `ErrInvalidObservation`

Remote API 404 is treated as “remote does not exist”; other remote errors are fatal.

#### `reconciler.go`

Important behavior:

- `ErrInvalidMode`
- `ErrSyncUnsupported`
- `ErrInvalidObservation`
- `ErrInvalidChecksum`
- `RemoteHistory` interface was added to restore Python previous-version behavior without coupling the reconciler to
  gomcapi.

`Reconciler` fields:

```go 
 type Reconciler struct { mode Mode checksum ChecksumFunc remoteHistory RemoteHistory }
```

Important design:

- `WithChecksumFunc` is used in tests.
- `WithRemoteHistory` injects previous-version lookup.
- If no `RemoteHistory` is configured, download mode remains conservative and returns conflict when it cannot prove the
  local content was previously uploaded.
- Empty local checksums are invalid and return `ErrInvalidChecksum`.
- Empty remote checksum is tolerated but not persisted.
- Observations are validated for consistency:
    - `Observation.RemotePath`
    - `Observation.Name`
    - `Observation.Dir`
    - `LocalEntry.RemotePath/Name/Dir`
    - `RemoteEntry.Path/Name/Dir`
    - existing `FileRecord.Path`
- Kind mismatch is conflict, including `KindUnknown` vs known kind.

Python behavior restored/improved:

- Python previous-version check logic in download mode has been modeled using `RemoteHistory`.
- Go still keeps remote/network calls out of the pure reconciler.

#### `walk.go`

Provides local, remote, merged, and remote-only walking.

Important types:

- `WalkNode`
    - `LocalPath`
    - `RemotePath`
- `ListDirFunc`
- `NodeListDirFunc`
- `RemoteDirectoryLister`
- `DirectoryRecordGetter`
- `WalkOptions`

Important functions:

- `Walk`
- `WalkNodes`
- `LocalListDir`
- `LocalNodeListDir`
- `RemoteListDir`
- `RemoteOnlyListDir`
- `MergedListDir`
- `MergedNodeListDir`
- `WalkAndReconcile`
- `WalkNodesAndReconcile`
- `DefaultIgnore`
- `ChainIgnore`
- `IsRemoteNotFound`

Important design:

- `Walk` is local-path oriented.
- `WalkNodes` supports local-only, merged, and remote-only recursive traversal.
- `WalkOptions.Translator` is used to synthesize local paths for remote-only entries.
- Remote-only recursion uses `WalkNode.RemotePath`.
- Do not add synthetic local path to `Observation`; use `WalkNode.LocalPath`.
- Invalid walk nodes return `ErrInvalidWalkNode`.
- Remote paths are validated with `projectpath.NormalizeRemote`.
- `.mc` and `.DS_Store` are ignored by default.

## Recent review/hardening work completed

We systematically reviewed and hardened:

1. `reconciler.go`
2. `observe.go`
3. `walk.go`
4. `filedb.go`

All tests are currently passing.

Important fixes from that review:

- `RemoteEntry.RemoteFileID` changed to `*int64`.
- Added `RemoteHistory`.
- Added `ErrInvalidObservation`.
- Added `ErrInvalidChecksum`.
- Added `ErrInvalidWalkNode`.
- Added `ErrInvalidRecord`.
- Empty local checksum is invalid.
- Empty remote checksum is not persisted.
- `MarkTransfer` now checks `RowsAffected`.
- `ClearRemoteByPath` was added because `Upsert` intentionally cannot clear nullable fields due to `COALESCE`.
- `RemoteEntryFromMCFile` validates remote API data.
- `WalkNodes` normalizes and validates nodes.

## Dependencies

Current main Go dependencies:

- `github.com/urfave/cli/v3`
- `github.com/materials-commons/gomcapi`
- `gorm.io/gorm`
- `gorm.io/driver/sqlite`

The project currently has:
`replace github.com/materials-commons/gomcapi => ../gomcapi` in go.mod

## Bugs found in Python while porting

- Python `rm.py` parser uses `prog='mc down'`, which is a copy/paste bug. The Go CLI uses `mc2 rm`.
- Python path handling sometimes treats Materials Commons remote paths as `Path` values and calls filesystem-like
  methods such as `resolve()`. The Go code separates local `filepath` handling from remote `path` handling.
- Python `local_to_remote_project_path` returns the original local path when it is outside the project. The Go version
  treats this as an error.
- Python DB manager could wrap `upsert_many` in a transaction while `upsert_many` also starts a transaction; the Go
  version avoids nested transaction behavior.
- Python code mixes observation, remote API calls, and reconcile decisions more tightly. The Go version intentionally
  separates these layers.

## What to do next

We are now ready to continue building command behavior on top of these foundational packages.

Good next targets include one of:

1. Implement `mc2 config`
    - Show global config.
    - With `--proj`, show project config.
    - Use existing `pkg/config`.
    - Preserve JSON layout.
    - Include tests.

2. Implement `mc2 ls`
    - Use `pkg/reconcile/walk.go`.
    - Use `--action` to show action and reason.
    - Probably start with local + remote merged listing.
    - No uploads/downloads yet.
    - Need output formatting.
    - Include tests with fake remote directory lister and fake filedb store.

3. Implement `mc2 proj`
    - Use gomcapi `ListProjects`.
    - Show projects user can access.
    - Mark current project if inside one.
    - Include tests with fake project lister.

4. Implement persistence of reconcile decisions
    - Decide how `Decision.Action` maps to DB operations:
        - `db_update`, `upload`, `download` likely call `Upsert`.
        - remote clearing requires `ClearRemoteByPath`.
    - This should be done carefully.

When continuing, please inspect the current Go files before generating changes. Run or reason through unit tests. Keep
all code well-tested and explain all file paths and reasoning.
