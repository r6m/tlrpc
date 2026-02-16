## Phase 0: Foundation & Tooling
**Duration**: 2 weeks
**Goal**: Establish development environment and project structure for gRPC-like MTProto framework

---

### Task 0.1: Project Bootstrap
**Agent**: Infrastructure Agent
**Documents**: README.md, CONTRIBUTING.md, Makefile, go.mod

**Specifications**:
- Initialize Go module at `github.com/r6m/tlrpc`
- Create directory structure as defined in README.md
- Set up Makefile with targets: `deps`, `build`, `test`, `lint`, `clean`
- Configure golangci-lint with strict rules (enable: govet, staticcheck, errcheck, ineffassign, gosimple)
- Create GitHub Actions workflow for CI (test on Go 1.24, 1.25)
- Add MIT LICENSE file

**Deliverables**:
```
tlrpc/
├── README.md (from previous spec)
├── CONTRIBUTING.md (from previous spec)
├── LICENSE
├── Makefile (from previous spec)
├── go.mod (from previous spec)
├── .github/workflows/ci.yml
├── .golangci.yml
├── .gitignore
└── docs/ (empty, for future)
```

**Verification**:
- [ ] `make deps` succeeds
- [ ] `make build` creates `bin/` directory
- [ ] CI pipeline passes on push
- [ ] `go mod tidy` produces clean go.sum

---

### Task 0.2: Testing Infrastructure
**Agent**: Testing Agent
**Documents**: Test patterns in CONTRIBUTING.md

**Specifications**:
- Create `internal/testutil/` package with helpers:
  - `Must(err)` - panic on error in tests
  - `RandBytes(n int) []byte` - cryptographically random bytes
  - `TempFile() *os.File` - temporary file for tests
  - `CaptureLogs() *LogCapture` - capture log output
- Create `testdata/` directory with sample TL binary files
- Set up test fixtures structure for future phases
- Add `testify` dependency to go.mod
- Create `make test-short` for quick tests

**Deliverables**:
```
tlrpc/
├── internal/
│   └── testutil/
│       ├── testutil.go       # Main helpers
│       ├── rand.go           # Random data generators
│       └── log.go            # Log capture
├── testdata/
│   ├── README.md             # Describes test data format
│   └── fixtures/             # Empty, populated in later phases
└── scripts/
    └── setup-testdata.sh     # Script to generate test fixtures
```

**Verification**:
- [ ] `go test ./internal/testutil/...` passes
- [ ] `make test-short` completes in <10 seconds
- [ ] All helpers have unit tests
