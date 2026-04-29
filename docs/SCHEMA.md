# CKG Schema (V0)

Schema version: **1.3** (v0.2 — E3 added two distributed-graph node kinds
(`Endpoint`, `MessageType`) and three handler/RPC edge kinds
(`listens_on`, `handles_message`, `rpc_calls`) for CKS deep-dive § 4.1
G5 Distributed Interaction. Pre-1.3 DBs lack these rows; the file-level
cache treats the bump as cache-invalidating, so the next `ckg build`
falls into the cold path on first run with this binary.)

A5 (1.0 → 1.1) reserved concurrency lock slots; A3 (1.1 → 1.2) added
incremental cache infrastructure (FK ON DELETE CASCADE on
edges/blobs/pkg_tree/topic_tree); E3 (1.2 → 1.3) adds distributed
topology nodes/edges. All bumps invalidate the file-level cache by
design.

## Node types (32)

`Package, File, Struct, Interface, Class, TypeAlias, Enum, Contract,
Mapping, Event, Function, Method, Modifier, Constructor, Constant,
Variable, Field, Parameter, LocalVariable, Import, Export, Decorator,
Goroutine, Channel, Mutex, IfStmt, LoopStmt, CallSite, ReturnStmt, SwitchStmt,
Endpoint, MessageType`

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

## Edge types (28)

`contains, defines, calls, invokes, uses_type, instantiates, references,
reads_field, writes_field, imports, exports, implements, extends,
has_modifier, emits_event, reads_mapping, writes_mapping, has_decorator,
spawns, sends_to, recvs_from, binds_to,
acquires_lock, releases_lock, accessed_under_lock,
listens_on, handles_message, rpc_calls`

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

## Confidence

`EXTRACTED` (direct from AST) | `INFERRED` (heuristic / dispatch) | `AMBIGUOUS` (unresolved).

See `spec-ckg-v0-prototype.md` §5.1–5.3 for full attribute and lang-coverage tables.
