# CKG Schema (V0)

Schema version: **1.1** (v0.2 — A5 bumped 1.0 → 1.1 to reserve concurrency
lock slots; first build under 1.1 invalidates the A3 incremental cache).

## Node types (30)

`Package, File, Struct, Interface, Class, TypeAlias, Enum, Contract,
Mapping, Event, Function, Method, Modifier, Constructor, Constant,
Variable, Field, Parameter, LocalVariable, Import, Export, Decorator,
Goroutine, Channel, Mutex, IfStmt, LoopStmt, CallSite, ReturnStmt, SwitchStmt`

LoopStmt uses `sub_kind ∈ {for, while, range, for_in, for_of}`.

`Mutex` is **slot-reserved** for B1 (Wave 5): the enum entry exists so
downstream caches/validators bake in the schema bump, but the parser
does not emit Mutex nodes yet. See `spec-ckg-v0.2.md` § 2.

## Edge types (25)

`contains, defines, calls, invokes, uses_type, instantiates, references,
reads_field, writes_field, imports, exports, implements, extends,
has_modifier, emits_event, reads_mapping, writes_mapping, has_decorator,
spawns, sends_to, recvs_from, binds_to,
acquires_lock, releases_lock, accessed_under_lock`

`acquires_lock`, `releases_lock`, `accessed_under_lock` are **slot-reserved**
for B1 (Wave 5) — same status as `NodeMutex` above. The viewer registers
styling for them but they are off by default (like other concurrency edges).

## Confidence

`EXTRACTED` (direct from AST) | `INFERRED` (heuristic / dispatch) | `AMBIGUOUS` (unresolved).

See `spec-ckg-v0-prototype.md` §5.1–5.3 for full attribute and lang-coverage tables.
