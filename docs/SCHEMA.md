# CKG Schema (V0)

Schema version: **1.23**.

> **Authoritative source of truth (single source).** The machine-readable
> node/edge type lists and their exact counts live in `pkg/types/enums.go`
> (`AllNodeTypes()` / `AllEdgeTypes()`); the live build's version constant is
> `internal/buildpipe/cache.go` `const SchemaVersion` (also written into each
> graph's manifest `schema_version`). This doc *describes* them — **when they
> disagree, code wins.** Other docs must link here (and to `enums.go`) instead
> of restating counts, so a schema bump updates exactly one place.

Version history (each bump invalidates the file-level cache by design):

A5 (1.0 → 1.1) reserved concurrency lock slots; A3 (1.1 → 1.2) added
incremental cache infrastructure (FK ON DELETE CASCADE on
edges/blobs/pkg_tree/topic_tree); E3 (1.2 → 1.3) added distributed
topology nodes/edges; E4 (1.3 → 1.4) added temporal commit nodes/edges;
G6v3 (1.4 → 1.5) added pending_refs persistence; P2 (1.5 → 1.6) added
context-propagation edges; Track C P1b (1.6 → 1.7) added the optional
`dispatch_kind` metadata column on edges (populated only for `invokes`
to disambiguate interface_method / func_value / method_value / closure);
Hunk-graph H1+H2 (1.7 → 1.8) added NodeHunk + has_hunk/adjacent/modifies;
schema-1.9-spec W1 (1.8 → 1.9) reused NodeEndpoint for TypeScript HTTP
server detection (no new enum literals), W2 appended `http_calls` (TS+Go
HTTP client call sites), W3b appended `grpc_listens_on` + `grpc_calls`
(Go gRPC server/client detection); within-language semantics Phase 4
(1.9 → 1.10) appended `NodeAwaitPoint` + edges `awaits` (W-B) and
`overrides` (W-C) as slot reservations ahead of the Phase 5 detectors;
W-C W11 V7 (1.10 → 1.11) added the `nodes.attrs` JSON-blob column
(`internal/persist/node_attrs.go`) carrying per-node markers; 1.11 → 1.13
internal bumps; P1 #4 (1.13 → 1.14) added `NodePolicy` + edge `governed_by`
(`pkg/policy`, opt-in `--policy-file`); P1 #5 (1.14 → 1.15) added
`NodeSecurityPattern` + edge `has_security_pattern` (`pkg/security`, opt-in
`--security-pattern-file`). `using_for` (Solidity using-for, W-C) is also
present in the current edge set. PR #23 (1.15 → 1.18) added full Go
signatures + qualified call resolution (1.16), promoted-method nodes (1.17),
and `writes_field` edges (1.18); symbol-identity Phase 1 (1.18 → 1.19)
populates `canonical_id` across all parsers — all Go node kinds (types/fields/
const/var/interface methods), and Solidity/TypeScript/proto using the relative
file path as qualifier (Solidity adds a `(paramTypes)` signature for overloads).
Refinement B1 (1.19 → 1.20) stops emitting `canonical_id` for the blank
identifier `_`; B2 + B4 + B3 (1.20 → 1.21) skip `canonical_id` for function-local `var`s (only
package-level const/var get one), drop proto's doubled `proto:` prefix, and
line-qualify (`@<line>`) any canonical_id shared by >1 node in one file. PR #31
(1.21 → 1.22) added the `nodes.simple_name` column (short last-segment name) so
suffix lookups use an indexed equi-join instead of a leading-wildcard LIKE.
ADR-0002 (1.22 → 1.23) gives primary (non-test-variant) Go packages deterministic
ownership of production files in `buildFileIndex` (was order-dependent first-seen-wins;
~17.5% of production files landed on a test variant), making the graph reproducible.
Full per-bump rationale: `internal/buildpipe/cache.go`.

## Node types (37)

*(Authoritative list: `pkg/types/enums.go` `AllNodeTypes()`. Count below is
kept in sync with that function — if you add a NodeType, update both.)*

`Package, File, Struct, Interface, Class, TypeAlias, Enum, Contract,
Mapping, Event, Function, Method, Modifier, Constructor, Constant,
Variable, Field, Parameter, LocalVariable, Import, Export, Decorator,
Goroutine, Channel, Mutex, IfStmt, LoopStmt, CallSite, ReturnStmt, SwitchStmt,
Endpoint, MessageType,
Commit, Hunk,
AwaitPoint, Policy, SecurityPattern`

LoopStmt uses `sub_kind ∈ {for, while, range, for_in, for_of}`.

`Mutex` is the B1 Stage 1 concurrency-analysis node, emitted by the Go
parser (`internal/parse/golang/concurrency.go`) for `sync.Mutex` /
`sync.RWMutex` declarations (struct fields, package-level vars,
function-body locals, embedded mutexes). `sub_kind ∈ {mutex, rwmutex}`.
`qualified_name` is `pkg.Type.field#mutex` for struct fields (the
`#mutex` suffix disambiguates from the same-position Field node) or
`pkg.func.localVar` for locals. Confidence is `EXTRACTED` with
typesInfo, `INFERRED` in AST-only mode. See `archive/spec-ckg-v0.2.md` § 2 and
`internal/parse/golang/concurrency_test.go`.

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
Capped at 10 most-recent commits per file by default. Subject text is
mined post-commit by the H4 issue-id pass (`temporal/issueid.go`): the
4 regex families (`GH-#`, `[PROJ-N]`, JIRA prefix, GitHub issue URL)
yield ticket IDs encoded back into the `Hunk.doc_comment` as a
`issues:GH-42;JIRA-7` string.

`Hunk` (Hunk-graph H1, schema 1.8): one contiguous block of changed
lines in one file in one commit, as defined by unified-diff `@@`
headers. `name` = `<sha12>:<file>:<idx>`, `qualified_name` =
`hunk:<full-sha>:<file>:<idx>` (idx = 0-based per-commit hunk position
so multiple hunks per commit get distinct IDs via MakeID).
`sub_kind` = `git`, `language` = `git` (same sentinel as Commit).
`start_line`/`end_line` = the hunk's @@ header new-file line range;
`start_byte` = 0 / `end_byte` = 1 sentinels (the patch text lives in
`blobs.source`, gzip-compressed; cap 64KB). `doc_comment` carries the
optional `issues:` encoding when H4 matches a ticket in the parent
commit's subject. Confidence semantics (hunk-graph.md §11.3, finalised
2026-05-09): `EXTRACTED` for HEAD-reachable hunks (the only kind H1
collects today); `AMBIGUOUS` reserved for unreachable hunks collected
via reflog/fsck (slot live, parser does emit them when run with
unreachable-collection enabled). The H3 EvidencePack assembler filters
to `confidence='EXTRACTED'` so the LLM never sees code paths that were
rolled back by force-push; the human Recovery panel surfaces AMBIGUOUS
explicitly.

`AwaitPoint` (W-B, schema 1.10): statement-level node for TypeScript
`await` expressions. One node per `await` site so graph queries can
answer "where does control yield, and to which AsyncCallSite?". The
schema-1.10 bump reserved the enum slot; the Phase 5 detector has since
**shipped** — the TS parser emits one `NodeAwaitPoint` per
`await_expression` (`internal/parse/typescript/async.go:56,98,114`),
paired 1:1 with an inbound `awaits` edge. See
`docs/design/ts-async-await-and-interface.md §2.1 + §3.2`.

`Policy` (P1 #4, schema 1.14): a governance/policy node loaded from an
opt-in `--policy-file` YAML (`pkg/policy`), linked to governed symbols via
`governed_by`. Empty policy file is the no-op default.

`SecurityPattern` (P1 #5, schema 1.15): a security-pattern node loaded from
an opt-in `--security-pattern-file` YAML (`pkg/security`), linked to matched
symbols via `has_security_pattern`. Additive, cold-only re-ingest.

## Edge types (43)

*(Authoritative list: `pkg/types/enums.go` `AllEdgeTypes()`. Count below is
kept in sync with that function — if you add an EdgeType, update both.)*

`contains, defines, calls, invokes, uses_type, instantiates, references,
reads_field, writes_field, imports, exports, implements, extends,
has_modifier, emits_event, reads_mapping, writes_mapping, has_decorator,
spawns, sends_to, recvs_from, binds_to,
acquires_lock, releases_lock, accessed_under_lock,
listens_on, handles_message, rpc_calls,
changed_in, blame,
timeout_path, cancellation_path,
has_hunk, adjacent, modifies,
http_calls,
grpc_listens_on, grpc_calls,
awaits, overrides, using_for, governed_by, has_security_pattern`

`acquires_lock` / `releases_lock` (B1 Stage 1): `Function`/`Method` →
`Mutex` for `mu.Lock()` / `mu.Unlock()` / `mu.RLock()` / `mu.RUnlock()`
calls. Receiver resolution uses `types.Info.ObjectOf` to confirm the
receiver's declaration object is the Mutex node — this defeats the
false-positive case where an unrelated type defines its own `Lock()`
method (`archive/spec-ckg-v0.2.md` § 2 R2.1). Read/write distinction lives on
the destination Mutex's `sub_kind`, not the edge type.

`accessed_under_lock` (B1 Stage 1): `Field` → `Mutex` for struct-field
identifiers accessed inside a Lock/Unlock pair in the same function
body (`internal/parse/golang/concurrency_underlock.go`). The (field,
mutex) pair is the edge key — repeated accesses inside the same
critical section collapse to one edge so multi-mutation methods don't
inflate the edge count. The viewer registers styling for all three
edges; they're off by default like other concurrency edges.

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

`timeout_path` (P2): self-loop on a Go Function/Method whose body calls
`context.WithTimeout` or `context.WithDeadline`. Deadline rolls into
`timeout_path` because both express a wall-clock budget — graph queries
that ask "which functions impose a time bound?" don't care about the
distinction. Confidence is `EXTRACTED` when typesInfo confirms the
`context` package binding, `INFERRED` in AST-only mode (matches
`context.WithTimeout` selector by name only). `cancellation_path` (P2):
self-loop on a Go Function/Method whose body calls `context.WithCancel`
or `context.WithCancelCause` (Go 1.20+). Distinct from `timeout_path`
because cancellation is event-driven, not deadline-driven. Multiple call
sites in the same function produce multiple edges (different `Line`).
**`retry_path` is reserved but NOT emitted** — heuristic too noisy for
V0; deferred until a typed retry primitive lands (backoff library
detection or annotated loops). Both edges off by default in the viewer.

`has_hunk` (Hunk-graph H1, schema 1.8): `Commit` → `Hunk`. One per Hunk;
"this commit produced this block of changed lines". Confidence mirrors
the Hunk's own (EXTRACTED for HEAD-reachable, AMBIGUOUS for unreachable
hunks added by the reflog-collection pass).

`adjacent` (Hunk-graph H1, schema 1.8): `Hunk` → `Hunk` between same-
commit, same-file hunks ordered by their @@ header start line. Provides
a deterministic "next-in-this-file" traversal so the EvidencePack
assembler can stitch a multi-hunk view of a commit's edits without a
separate ORDER BY query. Emitted only between hunks within one
(commit, file) pair — never across commits or files.
Out-of-scope clusterings (out of H1 scope, see hunk-graph.md §11.5):
`same_logical_change` across commits.

`modifies` (Hunk-graph H2, schema 1.8): `Hunk` → CodeNode (Function /
Method / Struct / Interface / Field / etc.) when the hunk's
`[start_line, end_line]` interval overlaps the CodeNode's interval
inside the same file. Whitelisted to FunctionLike + TypeLike + Field-ish
(13 node types) so noise-level statement nodes (CallSite / IfStmt / …)
don't blow up the edge count without retrieval signal. See
`docs/design/hunk-graph.md §4` for the whitelist rationale.

`http_calls` (schema 1.9 W2): caller `Function`/`Method` → `Endpoint`
when the function invokes an HTTP client (TS: `fetch`, `axios`, `useSWR`,
`useQuery`; Go: `http.Get` / `http.Post` / `http.NewRequest` /
`(*http.Client).Get/Post/Do`). Target resolution uses a 2-stage cascade
(schema-1.9-spec §6.9): (1) specific-verb lookup `http:METHOD /path`,
(2) wildcard fallback `http:* /path`; on miss the matcher synthesises
an `AMBIGUOUS` placeholder `Endpoint` (schema-1.9-spec §6.3 (B)). Path
matching is **exact** (§3.3 decision — false-positives across distinct
services with overlapping path suffixes are worse than the
false-negatives exact-match incurs in well-curated monorepos).

`grpc_listens_on` (schema 1.9 W3b): server-impl `Method` → `Endpoint`
when the file calls `pb.RegisterXXXServer(s, &impl{})`. Each method on
the impl receiver type whose name matches an RPC method on the generated
`XServer` interface emits one edge to a `grpc:Service.Method` Endpoint
(language=`go`, sub_kind=`grpc`).
`grpc_calls` (schema 1.9 W3b): caller `Function`/`Method` → `Endpoint`
when the body calls `<stub>.RpcMethod(ctx, req)` where `stub` was
assigned from `pb.NewXXXClient(conn)`. Like `http_calls`, on miss the
matcher synthesises an `AMBIGUOUS` placeholder Endpoint
(language=`external`). Confidence split (schema-1.9-spec §6.5 (C)):
typesInfo-confirmed → `EXTRACTED`, AST-only suffix-matcher → `INFERRED`,
unresolved stub type → `AMBIGUOUS` placeholder.

`awaits` (W-B, schema 1.10): edge for TypeScript async-suspension flow,
`Function`/`Method` → `AwaitPoint`. **Emitted** by the Phase 5 detector
(`internal/parse/typescript/async.go:120`), one per `AwaitPoint` (pair
invariant: `#AwaitPoint == #awaits`). Direction encodes "this function
suspends here". See
`docs/design/ts-async-await-and-interface.md §2.1 + §3.2`.

`overrides` (W-C, schema 1.10): edge for Solidity virtual/override
semantics. `Method` → `Method` between a child contract's method that
overrides a parent's `virtual` method (also modifier→modifier for
overridden modifiers). Direction = child → parent (Q4 decision in
solidity-inheritance spec §5.0). Distinct from `implements` (interface
satisfaction) because `overrides` is concrete-to-concrete virtual
dispatch resolution. **Emitted** by the Phase 5 detector
(`internal/parse/solidity/overrides.go`, PendingRefs from
`declarations.go`). See
`docs/design/solidity-inheritance-and-interface-dispatch.md §2.1 + §3.3`.

## Edge metadata: `dispatch_kind` (schema 1.7)

The `edges` table has an optional `dispatch_kind TEXT` column populated
only for the `invokes` edge type. Values: `interface_method`,
`func_value`, `method_value`, `closure` (Track C P1b). Empty string
otherwise. Migrate() ALTER-ADDs this column when opening a pre-1.7 DB
(see `sqlite.go::ensureDispatchKindColumn`). SELECT projections
enumerate columns explicitly (no SELECT *) so pre-1.7 readers tolerate
the column being absent.

## Confidence

`EXTRACTED` (direct from AST) | `INFERRED` (heuristic / dispatch) |
`AMBIGUOUS` (unreachable history — Hunk/Commit added by force-pushed
branches; filtered out of LLM-facing retrieval by the `llmSafeStoreReader`
wrapper, surfaced explicitly in the human Recovery panel).

