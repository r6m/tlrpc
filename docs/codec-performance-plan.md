# Codec and dispatch performance refactor

Status: implemented and validated in the working tree, 2026-09-10.

## Goals and scope

Improve generated-code clarity and measured CPU/allocation cost without changing
wire contracts, strict layer intervals, family interfaces, or concrete initializers.
All generated variants remain in category files (`types.go`, `requests.go`, etc.).
TLRPC and its own fixtures are in scope. Tgserver generation/upgrade remain deferred.

| Goal | Work | Gate |
| --- | --- | --- |
| P1: establish evidence | Preserve pre-change source; add reproducible small, payload-heavy, vector, dispatch and frame benchmarks | Repeated ns/op, B/op and allocs/op measurements with environment recorded |
| P2: correctness | Enforce 24-bit TL byte lengths; define receiver reset semantics | Boundary/malformed tests and repeated decoding clear absent optional fields |
| P3: codec engine | Concrete bounded decoder cursor and append encoder; one field-codec body per contract; read boxed IDs once | Exact bytes, layers, vectors, recursion and aggregate budgets preserved |
| P4: generated dispatch | Direct typed handler adapters and typed result loops | No reflect.Call in dispatch; post-interceptor request/result checks remain |
| P5: ownership experiment | Measure safe internal borrowed frame/body slices against owned copies | Retain only measured improvements with explicit lifetime/mutation tests |
| P6: integration | Regenerate framework fixtures; update API and performance docs | Full sequential tests, vet/build, targeted races, deterministic generation |

## Architecture decisions

- Generate concrete `*mtproto.Encoder`/`*mtproto.Decoder` field codecs. Public
  `SerializeTL(io.Writer)` and `DeserializeTL(io.Reader)` are supported stream
  entry points into that one implementation. There is no retained old codec loop.
- The runtime supplies bounded byte cursors and append encoders. Nested generated
  calls reuse the cursor, effective layer, and aggregate budget. Streaming callers
  use the same concrete codec with a stream-backed cursor.
- Generated family decoding reads the constructor ID once, selects by ID/layer,
  verifies the family, and invokes the field decoder. Bare fields invoke that same
  field decoder without a constructor prefix. Depth and node accounting occur once.
- Decoder methods return owned strings/bytes by default. Reused receiver fields are
  cleared before field decoding; on failure the receiver may be partially populated
  and must not be used as a successful result. No object pooling is introduced.
- Validate vector count, aggregate budget and known minimum payload size before
  reserving capacity. Cap speculative allocation for variable-size elements.
- Method descriptors use a fixed handler function signature. Generated adapters
  assert the request type and call the concrete service method directly. Startup
  interface checks may use reflection; per-request invocation does not.
- Generate exact scalar/vector/object result encoding after the interceptor result
  assertion. Share bounded encoding primitives, not a reflective object model.

## Zero-copy decision

Zero-copy is not a prerequisite for the contract model or a reason to delay the
codec refactor. Buffer cursors make it possible; ownership determines where it is
safe. Evaluate it during this work as a separate measured step.

The retained optimization borrows ciphertext only during synchronous decryption.
`DecodeFrame` does not mutate the input; decryption allocates separate plaintext
and returned message data remains owned. Keep normal service fields
owned. Queues, delayed operations and replay retain independent ownership or an
explicit ownership transfer. Do not return pooled storage while references remain.
Do not introduce unsafe string aliases or a public borrowed-request API by default.

Capture baseline, cursor/dispatch results, and isolated owned-versus-borrowed
comparisons. Record latency/allocations by payload size, not a universal speedup.
An optimization is retained when repeated measurements show a meaningful reduction
in work/allocations, safety tests pass, and it adds no disproportionate lifetime
complexity. Reject unexplained regressions and unmeasured zero-copy claims.

The isolated five-sample frame measurement retained this change: at 64 KiB,
median time fell from 117,488 to 111,350 ns/op, bytes allocated from 213,912 to
140,184 B/op, and allocations from 19 to 18. Tiny-frame time was essentially
unchanged. These are frame-decode microbenchmarks, not server throughput figures.
Raw evidence is in [baseline.txt](performance/baseline.txt) and
[frame-borrowed.txt](performance/frame-borrowed.txt). Broader wrapper/body borrowing
and a public borrowed-field API remain deferred; neither blocks this codec design.

### How the broader zero-copy architecture fits

| Technique | TLRPC decision and ownership boundary |
| --- | --- |
| Read into reusable buffers | A future transport optimization. Reuse only after synchronous consumers finish, or transfer ownership explicitly to the last consumer. Buffered network I/O already exists in `MTProtoConn`. |
| Parse by slicing the input | Implemented inside the byte decoder and synchronous ciphertext decryption. Spans used by primitive reads stay internal. |
| Keep fields as offsets or byte views | Defer for ordinary generated requests. Handlers can retain values, and a tiny retained view can keep an entire large frame alive. A future explicit borrowed API needs a documented lifetime and a clone/ownership boundary. |
| Forward payloads without re-encoding | Preserve already encoded payloads only when their exact contract, target layer, and ownership are unchanged. Outgoing MTProto session/message metadata and encryption are separate work; ciphertext is not generally reusable for another session. |
| Scatter/gather with `net.Buffers` | Benchmark separately at the final transport writer. Existing buffered, obfuscated, and WebSocket writers may prevent the specialized socket path; replacing two `Write` calls alone does not establish a gain. |
| Pool large buffers | Defer until profiles justify it. Use size classes and a maximum retained capacity; return storage only after queues, writes, and replay stop referencing it. Pooling reduces allocations, not the need to copy across independent owners. |
| Copy only at ownership boundaries | The current rule: no intermediate string payload copy in buffered decoding; one owned string/byte value crosses into generated service objects. Retained asynchronous data remains independently owned. |

Go documents `net.Buffers` as using an OS batch write only for certain connections
and platforms, not as a universal zero-copy operation
([net.Buffers](https://pkg.go.dev/net#Buffers)). `sync.Pool` caches temporary unused
objects and may discard entries; it does not track borrowers or release lifetimes
([sync.Pool](https://pkg.go.dev/sync#Pool)). Both can be added below the generated
contract model later, after transport benchmarks and ownership tests. Neither
requires changing generated family typing or making handlers fill view structs.

## Ownership and execution

Parent owns architecture, concrete cursor engine, correctness primitives, runtime
codec integration, ownership experiment, docs and final validation. Sol medium
agents own generator changes and direct runtime dispatch in disjoint files. Terra
medium owns reusable benchmarks and the frozen-source baseline. All subagents are
capped at Sol medium; independent reviews use the same cap. Full suites and
benchmarks run sequentially to avoid compiler pressure and timing interference.

## Completion record

P1–P6 are complete. [The performance report](performance/README.md) records the
measured gains, streaming decode tradeoff, allocation changes, raw samples, and
the alternating recheck of large-byte response timing.

- Concrete cursor codecs, one-read boxed dispatch, receiver reset, 24-bit byte
  limits, vector preallocation checks, direct handlers, and exact typed result
  callbacks are implemented. Duplicate generated codec emitters and reflective
  response normalization are removed.
- Synchronous ciphertext borrowing is retained with success/failure input-mutation
  and post-decode input-reuse tests. Service fields remain owned; no unsafe string
  conversion, object pool, or implicit borrowed request lifetime is introduced.
- Full sequential test suite: 790 tests across 26 packages passed.
- `go vet -p=1 ./...` and `go build -p=1 ./...` passed.
- Targeted race suite: 21 tests across root, `mtproto`, and `internal/runtime`
  passed, covering budgets, cursor ownership, layer routing, and interceptors.
- Byte decoding differential fuzzing passed 5,000 executions on the final code.
  A preceding time-bounded repeat ended with a harness context deadline and no
  failing input; the fixed-execution run completed successfully.
- Repeated `make generate` produced identical hashes across all 23 TLRPC fixture
  files. Type-domain, transport, legacy, and semantics guard scripts passed.

Sol medium agents implemented the generator and runtime adapters; Terra medium
implemented benchmarks and captured the frozen baseline. An independent Sol
medium cursor review found a stream-length issue, fixed with a regression test
before the final suite. No agent exceeded Sol medium. Tgserver generation and
upgrade remain the separately requested later phase.
