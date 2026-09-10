# Codec, dispatch, and ownership measurements

Measured 2026-09-10 on Go 1.26.4, darwin/arm64, Apple M4 Pro. The TLRPC
performance refactor is independent of tgserver generation or migration.

## Results and decision

The runtime buffer path and direct dispatch improve CPU and allocation cost
without exposing borrowed fields to services. Retain the bounded cursor engine,
generated typed response loops, direct handler adapters, and synchronous
ciphertext borrowing. Broader buffer pooling, offset-based public objects, and
scatter/gather transport changes remain separate measured work.

Selected medians from five samples:

| Workload | Before | After | Allocation change |
| --- | ---: | ---: | --- |
| Small runtime application dispatch | 1,071 ns | 693 ns | 39 → 30 allocs/op |
| Eight-int response vector encoding | 349 ns | 96 ns | 28 → 3 allocs/op |
| Buffered generated nested object/vector decode | 557 ns | 239 ns | 30 → 7 allocs/op; 672 → 352 B/op |
| Buffered generated 64 KiB string decode | 9,599 ns | 4,530 ns | 131,312 → 65,776 B/op |
| 64 KiB encrypted-frame decode, isolated ciphertext borrowing | 117,488 ns | 111,350 ns | 19 → 18 allocs/op; 213,912 → 140,184 B/op |

The frame change removes one ciphertext allocation and copy. Encryption primitives
and plaintext ownership are unchanged in that benchmark path. Small-frame timing
is essentially unchanged. This is a reduction in application copies and allocation,
not an assertion of kernel-level zero-copy or a completely copy-free MTProto stack.

## Tradeoffs and rechecks

Direct streaming decode is slower in this comparison: the small string case moves
from 78 to 182 ns/op, and the nested object/vector case from 379 to 455 ns/op.
The previous standalone stream entry point had no aggregate budget when given a
plain reader. The new cursor installs a default budget and performs byte, depth,
node and vector accounting even outside the runtime. This correctness cost is
retained deliberately; the two standalone stream cases have different protection
levels. Runtime buffer-path decoding had a shared budget before and after and is
the appropriate comparison for framed application traffic. Both stream and buffer
entry points now execute the same generated field body.

The larger cursor also increases fixed allocation bytes for a few tiny encodings:
scalar responses use 176 rather than 168 B/op while dropping from six allocations
to two. Reused-buffer small string encoding uses 112 rather than 80 B/op while
dropping from seven allocations to one. These are explicit retained space/time
tradeoffs, not universal allocation-byte improvements.

The first sweep showed a 22% timing regression for a standalone 64 KiB byte
response despite fewer allocations. An alternating before/after recheck with five
500 ms samples did not reproduce it: medians were 4,949 vs 4,866 ns/op, with
overlapping sample ranges. Treat large-byte response speed as unchanged; retain
the reproducible allocation reduction (nine to three). See
[the alternating recheck](large-bytes-recheck.txt). The already-direct generated
callback microbenchmark is also essentially unchanged; the dispatch gain comes
from removing runtime reflection, not making a Go method call itself faster.

## Methodology and reproducibility

The baseline is a frozen source copy made before this performance refactor, after
the distinct-layer-contract refactor, at
`/tmp/tlrpc-perf-baseline-g_vzb16e/tlrpc`. It is a local experimental artifact;
raw results are checked in so the report does not depend on retaining that directory.
The tree was already dirty, so a Git commit ID alone does not identify the baseline.

- Five samples per case, `-benchmem -benchtime=200ms -cpu=1 -p=1`; medians are
  descriptive, with no confidence-interval or end-to-end throughput claim.
- Baseline and final timing ran sequentially, without another benchmark process.
  Normal desktop scheduling and GC still introduce noise, especially for large
  payloads. The large-byte recheck alternates process order.
- Runtime application dispatch asserts a successful `RPCResult`, so an error
  intent cannot masquerade as a fast successful operation. It uses a handwritten
  codec fixture to isolate runtime changes.
- Generated codec benchmarks cover small, 1 KiB, and 64 KiB strings plus a boxed
  peer and byte-vector object from the TLRPC Telegram fixture. Stream encoding
  reuses a `bytes.Buffer`; the buffer-path comparison uses a fresh output buffer.
- Baseline buffer-path decoding uses the existing budget reader; final decoding
  uses `NewDecoderBytes`. Both produce owned values. Final encoding uses the
  bounded append encoder in place of a fresh `bytes.Buffer`.
- The benchmark name `BenchmarkEncodeMethodResponse` remains stable for comparison;
  final source uses `EncodeTypedResponse` callbacks and exact typed loops. The
  `nested_vector` case is TL `Vector<bytes>` (`[][]byte`), not `Vector<Vector<int>>`.
  Actual nested vector correctness has a separate exact-byte generated test.
- API call sites in the frozen benchmark use the old handler/result ABI; current
  benchmark call sites use the new ABI with the same payloads and wire workloads.

Raw evidence: [baseline](baseline.txt), [final](final.txt),
[isolated ciphertext borrowing](frame-borrowed.txt), [environment](environment.txt).
[All median comparisons](comparison.md) preserve the original sweep, including
regressions and the large-byte result qualified by the recheck above.

Commands are recorded with each raw file. Run the final root benchmark selection
from `final.txt`, and the internal runtime selection from `frame-borrowed.txt`.
Do not run timing concurrently with generation, test suites, or other benchmarks.

## Ownership architecture

The user's proposed reusable-buffer/slice/view/writev architecture is compatible
with the cursor design, but each optimization belongs to a particular owner.
See [the technique-by-technique decisions](../codec-performance-plan.md#how-the-broader-zero-copy-architecture-fits).
Services keep ordinary Go fields, concrete pointers for unchanged singleton
boxed families, and stable interfaces for families with multiple constructors
or distinct contracts. Internal
slicing avoids intermediate work; converting the final field to owned storage
is the explicit boundary that allows handlers to retain it safely.

## Verification

Full suite: 790 tests across 26 packages passed. This includes generated-package
compilation, strict layer selection, nested vector bytes, receiver reuse, runtime
interceptor contracts, codec limits, and network compatibility tests. A bounded
5,000-execution byte-decoder differential fuzz run also passed. Final vet, build,
targeted race and deterministic-generation checks are recorded in the
[completion plan](../codec-performance-plan.md#completion-record).
