# CKG Schema (V0)

Schema version: **1.4** (v0.2 — E4 added one temporal node kind (`Commit`)
and two git-history edge kinds (`changed_in`, `blame`) for CKS deep-dive
§ 4.1 G6 Temporal. Pre-1.4 DBs lack these rows; the file-level cache
treats the bump as cache-invalidating, so the next `ckg build` falls
into the cold path on first run with this binary.)

A5 (1.0 → 1.1) reserved concurrency lock slots; A3 (1.1 → 1.2) added
incremental cache infrastructure (FK ON DELETE CASCADE on
edges/blobs/pkg_tree/topic_tree); E3 (1.2 → 1.3) added distributed
topology nodes/edges; E4 (1.3 → 1.4) adds temporal commit nodes/edges.
All bumps invalidate the file-level cache by design.

## Node types (33)

`Package, File, Struct, Interface, Class, TypeAlias, Enum, Contract,
Mapping, Event, Function, Method, Modifier, Constructor, Constant,
Variable, Field, Parameter, LocalVariable, Import, Export, Decorator,
Goroutine, Channel, Mutex, IfStmt, LoopStmt, CallSite, ReturnStmt, SwitchStmt,
Endpoint, MessageType,
Commit`

LoopStmt uses `sub_kind ∈ {for, while, range, for_in, for_of}`.

`Mutex` is **slot-reserved** for B1 (Wave 5): the enum entry exists so
downstream caches/validators bake in the schema bump, but the parser
does not emit Mutex nodes yet. See `spec-ckg-v0.2.md` § 2.

`Endpoint` (E3): an HTTP/RPC route entry point. `qualified_name`
encodes the protocol prefix (e.g. `http:/users`, `rpc:Foo.Bar`); `name`
is the bare route. `sub_kind ∈ {http, rpc, ...}`. Emitted by the Go
parser for `http.HandleFunc` / `http.Handle` / `(*ServeMux).HandleFunc`
/ `(*ServeMux).Handle` calls with a string-literal route. Dynamic routes
are skipped (a runtime trace is the right hammer for those).

`MessageType` (E3): a request/response type a handler dispatches on.
`qualified_name` is `pkg.TypeName` for in-source types, or
`rpc:Service.Method` for placeholder targets of unresolved
`client.Call("Service.Method", …)` invocations. `sub_kind ∈
{rpc_request, rpc_method}`.

`Commit` (E4): a git commit that touched one or more source files.
`name` = first 12 chars of the SHA, `qualified_name` = `commit:<full-sha>`,
`signature` = `<unix-author-time>: <subject>` (truncated to 100 chars),
`sub_kind` = `git`, `language` = `git` (sentinel — keeps audit's
per-language file-set diff clean), `file_path` = the build root's
repo-relative path (stable across builds inside the same repo),
`start_line`/`end_line` = 1 (commits have no source range). Emitted
by the post-Build temporal pass (`internal/buildpipe/temporal.go`)
from a single `git log --raw --no-renames` invocation per build.
Capped at 10 most-recent commits per file by default.

## Edge types (30)

`contains, defines, calls, invokes, uses_type, instantiates, references,
reads_field, writes_field, imports, exports, implements, extends,
has_modifier, emits_event, reads_mapping, writes_mapping, has_decorator,
spawns, sends_to, recvs_from, binds_to,
acquires_lock, releases_lock, accessed_under_lock,
listens_on, handles_message, rpc_calls,
changed_in, blame`

`acquires_lock`, `releases_lock`, `accessed_under_lock` are **slot-reserved**
for B1 (Wave 5) — same status as `NodeMutex` above. The viewer registers
styling for them but they are off by default (like other concurrency edges).

`listens_on` (E3): handler function/method → `Endpoint` it serves
(net/http registration patterns). `handles_message` (E3): handler
function/method → `MessageType` it dispatches on (matched on the net/rpc
handler signature `func (T) M(args A, reply *R) error`). `rpc_calls`
(E3): caller function → `MessageType` placeholder for the
`Service.Method` target of `client.Call(...)`. All three are off by
default in the viewer (opt-in via filter UI).

`changed_in` (E4): any symbol whose file was touched by a commit →
that `Commit`. **File-level heuristic** (V0 simplification): every node
sharing a touched file emits one edge per commit, not per source line.
Line-level blame (true `file:line → commit`) is deferred to G6 Phase 2.
`blame` (E4): `File` node → its most recent commit (V0 simplification
of the spec's `file:line → commit (마지막 수정)`). Both are off by
default in the viewer; toggle via filter UI.

## Confidence

`EXTRACTED` (direct from AST) | `INFERRED` (heuristic / dispatch) | `AMBIGUOUS` (unresolved).

See `spec-ckg-v0-prototype.md` §5.1–5.3 for full attribute and lang-coverage tables.
