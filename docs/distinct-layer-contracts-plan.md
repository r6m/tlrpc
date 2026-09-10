# Distinct layer contracts refactor

Status: TLRPC refactor completed and validated, 2026-09-10. This document
records stable boxed-family interfaces, source-compatible concrete initializers,
and generated codec dispatch. Tgserver generation and upgrade are deferred.

## Goal

Generate stable boxed TL result-type interfaces and one concrete Go struct per
distinct constructor contract, with explicit method lifetimes and layer-aware
runtime validation. Keep one generated package and
typed service boundary; introduce no canonical RPC model or Telegram semantics
into TLRPC. Coordinate the consuming server through its
`docs/distinct-layer-contracts-plan.md` migration plan.

## Architectural decisions

The boxed-family/interface choice is locked by the user. Existing concrete
initializers must remain source-compatible when another boxed constructor
variant is introduced; this is a required acceptance condition, not merely a
convenient example. Runtime layer legality remains a separate condition.

### Contract identity and naming

- Constructor identity includes its TL name, ID, ordered fields, flag words and
  bits, generic/bare/vector structure and result family. Boxed references identify
  the stable TL family, not its current constructor shapes. Bare references
  include their statically resolved concrete layout. Added optional fields are
  changes even with the same ID.
- Exclude source locations, comments, availability ranges and naming provenance
  from contract equality. Do not use annotated AST equality as contract identity.
- Reuse unchanged contracts across snapshots. The baseline uses unsuffixed constructor-derived Go
  names from the first multi-layer generation, even for a singleton family; subsequent distinct contracts get `Layer<N>` using first introduction.
  Preserve historical provenance names such as `Layer172` where explicit.
- If an identical contract disappears and returns, reuse its type and retain
  separate availability intervals. A gap must not become a valid interval.
- In multi-layer output, every application-defined boxed TL result family gets
  a stable unsuffixed interface, including families with only one constructor.
  This applies even when the selected history currently contains only layer
  228: adding a second layer must not be the trigger for introducing interfaces.
  Use the existing `Type` suffix convention: `ChatAdminRightsType`. Concrete
  constructors implement their family's marker and TL codec methods. Primitive
  scalars, bytes, flags and runtime-owned controls keep their existing mappings.
- Parent fields, boxed vector elements and boxed method results use these
  interfaces. An optional-field change creates `ChatAdminRightsLayer229` but
  leaves `Channel.AdminRights ChatAdminRightsType` unchanged. A parent gets a
  variant only when its own declaration or a bare dependency changes; changed
  boxed descendants never trigger a parent or request-type cascade.
- Interfaces describe a TL family across history, not an anonymous `any` field
  or a field-specific union. Generated validation accepts only known concrete
  members of that family and checks their layer intervals. A Go interface type
  assertion alone cannot establish wire validity.
- This preserves each concrete constructor's exact fields and family-level Go
  typing. It deliberately permits constructing a parent containing a wrong-layer
  child in Go; validation rejects that graph before wire output. No compile-time
  guarantee of whole-graph layer safety is claimed.

### Bare references and recursive graphs

- Preserve the schema's boxed/bare distinction. A boxed value carries an ID;
  a bare or constructor-specific reference fixes the layout from its declared
  type and effective layer. Never add a constructor tag to a bare encoding.
- Keep bare references statically typed to the resolved constructor variant.
  Propagate changes only through bare dependencies, including bare vector
  elements. Stop at every boxed family interface.
- Resolve bare layouts independently in each snapshot. Reject an ambiguous or
  unsupported bare reference at generation with an owner/layer diagnostic;
  never choose a constructor by payload guessing or silently box it.
- Resolve acyclic bare dependencies by structural contract signatures. Recursive
  bare layouts and replacement of a bare family's constructor name are explicitly
  rejected. Boxed recursion keeps stable family
  identity; generated codecs enforce depth/node limits and reject cyclic output.
- Bare fields and bare vector members have dedicated generated codecs that omit
  the constructor tag. Bare method results (including bare elements inside result
  vectors) are currently rejected: the fixed response encoder cannot represent
  their wire distinction. No boxed fallback is generated.

### Generated example

```go
type ChatAdminRightsType interface {
    ConstructorID() uint32
    TLName() string
    SerializeTL(io.Writer) error
    DeserializeTL(io.Reader) error
    isChatAdminRightsType()
}

type Channel struct {
    AdminRights ChatAdminRightsType
}

// Each struct has its own exact fields and generated codec methods.
func (*ChatAdminRights) isChatAdminRightsType() {}
func (*ChatAdminRightsLayer229) isChatAdminRightsType() {}
```

Both rights variants implement one stable family. No `ChannelLayer229` is
generated solely because its boxed rights member changed. Unchanged handlers
can choose either member using the effective layer; live output uses the
recipient binding's layer, independently of the originating method.

The same application initializer must compile against the baseline and expanded
multi-layer packages without edits:

```go
channel := &Channel{
    AdminRights: &ChatAdminRights{ChangeInfo: true},
}
```

Construct concrete values and assign them to the named family interface; never
require callers to construct an interface or wrap each assignment. Adding 229
does not change this initializer's concrete value into a 229 representation.
An unchanged method's output builder may select a newer value or explicitly
project it. Migrating an existing concrete field to an interface can still
require changes to direct field reads, slice types and helper signatures; this
guarantee covers compatible concrete assignments, not all previous API usage.

### Request types and handler contracts

- Track request wire identity separately from full service signature identity.
  A result-only change reuses an unchanged request struct but produces another
  handler when the declared result contract changes, such as a different boxed
  family, primitive, vector structure or resolved bare return layout.
  Represent request-contract, response-contract and handler-contract references
  explicitly in parser metadata. The existing single `FuncDecl.VariantLayer`
  cannot name both a shared request and a newly versioned handler. Update
  `internal/generator/typeutil.go` so each name follows the correct reference.
- Full method identity includes constructor ID, request shape and resolved Go
  return shape. Resolve return references as well as input references before
  deduplicating methods. Boxed input/output references use stable family identity;
  their changing members do not change the enclosing method's identity.
- Adding or replacing a constructor in an unchanged boxed result family creates
  no extra handler, including when that family was previously a singleton.
  A changed method ID or own request layout still creates a distinct handler.
- Both typed handlers may call private application business operations. TLRPC
  generates no business input/result conversion layer.

### Availability and historical exceptions

- Replaced or removed method variants close at the preceding layer by default,
  whether IDs differ or remain equal. Unique IDs do not imply eternal validity.
- Wire constructors also retain actual snapshot availability. Historical types
  remain generated for old layers; membership in a shared union grants no
  permission to decode or encode them at a newer layer.
- Keep naming provenance independent of acceptance. The explicit syntax is an adjacent `// @tlrpc accept-layers 228-229` on a
  historical method declaration, alongside `// @tlrpc variant-layer 172`.
  Allow a comma-separated list of bounded inclusive ranges for gaps. Do not
  infer acceptance from the provenance suffix or provide an unbounded wildcard.
- This annotation is the authoritative availability for a historical
  declaration, not an addition to an implicitly unbounded snapshot range.
  A historical annotated declaration is an exception,
  not an ordinary baseline declaration implicitly inherited forever. Require
  its accepted intervals to be explicit; reject incomplete or invalid metadata.
  Bind annotations to their declaration occurrence/source position, not a
  `(name, ID)` key that aliases historical same-ID shapes. Require positive
  ordered disjoint ranges; reject duplicates, overlap and reversed ranges, and
  coalesce adjacent ranges after validation.
- Validate ranges against the full supplied schema history, then intersect them
  with the selected generation target/history. Selecting a single older target
  must not invalidate an annotation also covering a later supplied snapshot.
  Normalize adjacent intervals, reject overlaps between different contracts with the same ID, and
  reject exceptions whose nested input or return contract cannot be resolved
  for each target interval. Extending supported layers requires reviewing the
  bounded historical allowlist.
- For single-target generation, select only exceptions accepted at that target;
  historical provenance must not accidentally widen the selected schema.
- Runtime normalizes an unset layer to the package maximum, retains existing
  future-layer capping, and passes a concrete nonzero effective layer through
  dispatch, projection, encoding and request-scoped sending for layered schemas.
  Standalone generated APIs retain documented zero-as-base behavior; runtime
  must never accidentally rely on that standalone default. Unlayered schemas
  keep their existing zero-layer semantics. Test both defaults separately.
  Layers remain a
  compatibility mechanism, not authorization or a trusted client-version claim.

### Dispatch and codec enforcement

TLRPC generates the dispatch machinery. Existing boxed union serialization
already calls a value's `SerializeTL` method through its interface, and union
decoding uses `NewConstructorForLayer` followed by a family assertion and the
selected value's `DeserializeTL`. Extend those paths to every boxed family,
including singletons, while enforcing the strict interval and family rules
below. Application handlers do not implement serialization type switches.
Decoding cannot inspect a Go dynamic type before constructing a value: the
generated factory chooses it from the wire ID and effective layer, then verifies
the expected family. Encoding uses interface method dispatch; a handwritten
`switch value.(type)` is unnecessary for selecting the concrete serializer.

- Select using `(constructor ID, effective layer)` before request decoding,
  interceptors or service invocation. No matching interval returns the existing
  correlated `METHOD_NOT_FOUND`; no fallback or payload-based layer guessing.
- Same-ID replacements have disjoint intervals and exactly one decoder for a
  given layer. Old bytes that also satisfy the new contract are a new-contract
  request, not evidence that the historical handler should run.
- Input method factories and nested constructor factories enforce the same
  intervals as registration. Reject unknown flags and trailing bytes.
- Boxed nested decoding selects by expected TL family, constructor ID and
  effective layer. Reject IDs belonging to another family even when globally
  known. Encoding selects the concrete value's fixed codec and validates both
  family membership and target-layer availability. The ID alone cannot select
  between `ChatAdminRights` variants that retain the same wire ID.
- Fixed-contract encoders do not silently omit fields to impersonate another
  contract. Reject unavailable variants and nested union members at the target
  layer, including direct replies, vector roots and live pushes.
  Generated `SerializeTL` and `DeserializeTL` enforce full interval sets even
  when invoked directly, bypassing factories and projection.
- Add generated `MethodDesc.EncodeResponse func(any, int, EncodeLimits)
  ([]byte, error)`, using `EncodeTypedResponse[ExactGoReturnType]` and a generated
  cursor callback. It checks the post-interceptor value's declared result type,
  then encodes once using the effective layer and byte budget. Generated nested
  codecs enforce family membership and variant intervals. This combines result
  validation with encoding instead of serializing twice. Registration rejects
  descriptors missing the callback; no legacy fallback is retained.
- Bind the effective layer and configured encoding limits into `runtimeSender`
  in `sender.go`. Request-scoped `Sender.Send` uses the layer-aware bounded path,
  just as projected publishing does. Preserve session/lease fences on delivery.
- An explicitly accepted historical request does not downgrade output. Its
  handler return contract and all nested results must fit the effective layer.
- Multiple descriptor rows may reference one handler/type for disjoint valid
  intervals. Never duplicate a Go declaration just to express availability.

### Projection

- Retain generated structural projection between explicit variants. It returns
  new target objects, never mutates the source, and validates the result graph.
- Automatic conversion may copy compatible fields and add zero optional fields.
  Dropping nonzero fields, required-field synthesis, field-type changes and
  semantic replacements require an application hook or an explicit error.
- Hook replacements re-enter target validation/projection. Skip only that
  replacement root's hook to prevent loops; traverse nested hooks normally.
- Projection is optional boundary conversion, not a canonical TL object model.
  Applications can construct the appropriate variant directly from domain state.
- When only a boxed child changes, clone the same parent Go type and replace
  that interface-valued child with its projected concrete variant. Preserve
  boxed vector element interfaces, typed nil handling and source immutability.
- Preserve bounded traversal for untrusted input/output and test cycles/depth,
  vectors, nils, union members and hook replacements.

### Generated files and scope

- Keep one package and the existing category files: `types.go`, `interfaces.go`,
  `requests.go`, `services.go`, `register.go`, `codec.go`, `projection.go` and
  supporting metadata files. Put all layer variants in those files. Type names
  retain `Layer<N>` provenance; do not partition filenames by layer.
- Reuse the existing writer; no layer-file partitioning framework or ownership
  manifest is needed for this refactor. Deterministic fresh-directory generation
  remains the drift gate.
- The user authorizes breaking changes. Delete superseded superset and lifetime
  logic, update tests/fixtures and docs, and add no old-API compatibility shims.
  Ordinary single-target generation is still a supported use case, not a legacy
  API that must be preserved byte-for-byte.
- Current implementation scope is TLRPC only. Regenerate framework fixtures and
  examples as needed to validate its API. Defer the tgserver schema regeneration,
  dependency upgrade, service migration and server/client integration gates.

## Implementation sequence and ownership

Freeze the parser's family, request, response and handler references and
interval-set representation before delegating generator work. Freeze the generated
`MethodDesc.EncodeResponse` callback before runtime work. Keep the existing
category-file writer and coordinate CLI interface emission with semantic
generation. Each shared file has one assigned owner; tests follow the owner of
the code they exercise. The server plan's P1-P8 work packages define the
cross-repository dependency and exclusive write scopes.

The first milestone is a generated small schema package that proves the locked
initializer compiles both before and after the next layer is supplied. Full
Telegram regeneration begins only after that package proves stable boxed family
interfaces, correct same-ID decoding and lifecycle enforcement. Generated Go
compilation is part of each semantic milestone, not deferred to server migration.

All implementation/review agents are capped at `gpt-5.6-sol`, medium reasoning.
Use Sol medium for parser, generator/runtime correctness and final architecture
review; Terra medium for bounded emission/consumer changes; Luna medium for
documentation and evidence collection after contracts are frozen.
No agent may silently escalate the model or reasoning effort.

1. **Freeze the contract and failing fixtures — Sol medium.** Replace the
   contradictory requirements that additive fields share supersets and that
   all response-only changes preserve handlers. Add synthetic 228/229/230
   fixtures for optional additions, stable singleton boxed interfaces, unchanged
   parents/handlers, bare-only propagation, declared result changes,
   removal/reappearance and bounded historical exceptions.
2. **Resolve identities and lifetimes — Sol medium.** Change
   `internal/parser/layered.go`, AST metadata, baseline/delta parsing and
   validation. Remove `additiveFlagExtension`/`mergeAdditiveConstructor` from
   representation selection; remove unique-ID lifetime reopening from
   `normalizeUniqueIDRanges`. Implement independent contract identities and
   interval sets, boxed family references, bare dependencies, result references
   and historical input policy. Replace snapshot-dependent singleton/union Go
   mapping with stable boxed-family mapping in multi-layer generation.
3. **Emit fixed contracts in existing files — Sol medium.**
   Update types, requests, services, registration, codec and projection
   generators. A single owner handles the parser/generator semantic boundary;
   keep CLI orchestration separate from parser/generator ownership.
   Update `cmd/tlrpc-gen/main.go` and `internal/generator/writer.go`; generate
   fixtures rather than editing generated files.
   Reuse existing union interface serialization and layer-aware factory decoding
   for singleton boxed families. Compile the same application initializer
   against one-layer and extended multi-layer output before consumer migration.
4. **Prove runtime enforcement — Sol medium.** Review `server.go`,
   `runtime_application.go`, `method_layers.go`, `types.go`, `sender.go`, codec entry points and runtime
   wrapper/session paths. Retain the existing dispatch architecture and error
   mapping; change only missing enforcement. Prove decoder/interceptor/handler
   invocation counts remain zero for rejected method intervals.
5. **Integrate tgserver — separate consumer phase.** Regenerate against the exact
   local TLRPC candidate, migrate typed services and projection, and run its
   mixed-layer/persistence gates before freezing a release candidate.
6. **Independent review and release evidence — Sol medium + Luna medium.**
   Review semantic diffs separately from generated file movement, run complete
   sequential tests/vet/build and architecture checks, and replace stale docs.
   Record exact source revisions and generation provenance. A breaking pre-1.0
   minor release is expected; choose its number after checking actual tags.
   Publishing or deployment is a separate action, not a planning deliverable.

## Acceptance

- Byte-identical repeated generation and clean-directory drift in existing
  category files; compile examples and fixtures. No per-layer Go files.
- Exactly one type per distinct contract; same-ID optional addition yields two
  concrete types and fixed codecs; unchanged third-layer reuse is proven.
- Same-ID rights variants share one family interface; the parent declaration,
  request type, method result signature and handler count stay unchanged when
  only a boxed descendant changes. Prove singleton-to-multiple evolution and
  boxed vector/recursive parents do not introduce redundant variants.
- Compile an identical application fixture containing
  `Channel{AdminRights: &ChatAdminRights{ChangeInfo: true}}` against baseline-only
  multi-layer output and output extended with layer 229. No fixture edits,
  wrapper conversions or parent variants are permitted. Separately verify
  old/new values' target-layer validation and explicit projection behavior.
- Bare child changes propagate to affected static parents, vectors and methods;
  byte fixtures prove no extra tags are emitted. Invalid bare layouts reject
  generation. Boxed fields reject wrong-family and wrong-layer nested values.
- Different result families and primitive/vector structure split handlers while
  unchanged requests remain shared. Unsupported bare result layouts fail
  generation explicitly. Constructor
  evolution within one boxed result family alone never splits handlers.
- Replacement/removal closes old method IDs; direct and wrapped forged calls
  cannot enter old handlers; explicit historical calls succeed only in their
  bounded ranges; new IDs fail on old layers. Same-ID overlap fails generation
  or registration, and no decode fallback exists.
- Removal/reappearance creates disjoint availability without duplicate types.
- Structural projection round trips preserve supported values, refuse implicit
  lossy conversion, preserve sources and validate hook-produced nested graphs.
- Input/output availability is consistent across factories, direct codec calls,
  RPC dispatch, vectors and pushes; persistence compatibility never widens wire
  acceptance.
- Post-interceptor result validation rejects a wrong concrete result, wrong
  vector element and wrong-layer union member. Request-scoped sending honors
  the binding layer and output budget, including before explicit negotiation.
- Single-layer custom and Telegram fixtures remain valid; no regression in
  wrappers, decode limits, replay, reconnect, leases, barriers or push fencing.

Run focused tests first, then `GOWORK=off rtk go test -p=1 ./...`,
`GOWORK=off rtk go vet -p=1 ./...`, and
`GOWORK=off rtk go build -p=1 ./...`. Run focused runtime/codec race tests and
the existing Makefile architecture guards. Compile packages sequentially;
parallelize independent reviews, not resource-heavy full suites.

## Completion record

Framework implementation is complete in the working tree. Validation performed:

- `go test -p=1 ./... -count=1`: 782 tests passed across 26 packages.
- `go vet -p=1 ./...` and `go build -p=1 ./...`: passed.
- Focused response, sender, registration and codec-budget race tests: 15 passed.
- All four Makefile architecture guards and `git diff --check`: passed.
- Each of the three checked-in framework fixtures matches two fresh generations
  byte for byte (23 category files total). Layered CLI fixtures separately prove
  deterministic output and unchanged baseline/expanded application initializers.
- Generated runtime fixtures verify same-ID variants, projection, strict method
  lifetimes, removal/reappearance, and exact bare-field/request wire bytes.

Bare method results, recursive bare dependencies, and changes to the constructor
name of a bare-referenced family are explicitly unsupported. A historical bare
request must resolve to one layout across all its accepted intervals. These
cases fail generation instead of silently changing the wire contract.

Only TLRPC's own fixtures and examples were regenerated. Tgserver's schema,
generated contracts, dependency upgrade, and service migration remain deferred.
No release, commit, deployment, or consumer-compatibility claim is made.
