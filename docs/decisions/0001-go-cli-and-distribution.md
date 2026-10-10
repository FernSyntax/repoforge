# ADR-0001: Go, a single CLI, and static distribution

- Status: accepted
- Date: 2026-10-10
- Spec: spec.md §9, §19, §16.4
- Implementation: `cmd/repoforge/`, `internal/`, `go.mod`

## Context

The standard has to be enforceable, which means the checks cannot live
in a shell script a contributor can quietly edit. It also has to run on
a maintainer's laptop, in CI, and inside any language's repository —
including repositories that are not Go.

The alternatives were a polyglot script suite, a Python CLI, or a
compiled single binary in a different systems language.

## Decision

RepoForge is a single Go CLI, `repoforge`, distributed as static,
version-stamped binaries plus `go install` from the tagged source.
Cross-compilation covers linux/darwin/windows on amd64 and arm64, with
windows/arm64 excluded because there is no demand.

Go is chosen for a standard library that already covers HTTP, JSON, and
glob matching, so YAML is the only third-party dependency (P11); for a
single static binary with no runtime to install; for `gofmt` and
`go vet` as built-in quality gates; and for a module graph small enough
to audit.

## Alternatives considered

| Alternative | Why it was rejected |
| --- | --- |
| Shell script suite | No type safety, no testable structure; validation logic would drift per repository. |
| Python CLI | A runtime dependency and a packaging story that is worse on a clean Windows machine. |
| Rust CLI | Viable, but throughput and standard-library fit are better in Go for an I/O-bound tool. |

## Consequences

- A Go toolchain is required to build from source. Prebuilt binaries
  remove that requirement for users.
- `internal/template` deliberately builds no Go code: the generated
  baseline is language-agnostic, so the builder never assumes a Go
  consumer.
- Test coverage is bounded by Go's toolchain: table-driven unit tests,
  race detection, and bounded fuzzing.

## Security, maintainability, and operational implications

- Static binaries carry no dynamic-linker surprises and no runtime to
  patch. Version, commit, and date are stamped at build time so a
  binary reports its exact source.
- One language means one toolchain, one lint configuration, and one
  dependency-auditing surface for contributors.
- Operationally the release workflow must produce and verify checksums,
  an SBOM, and provenance, because users download binaries.

## Links

- Tests: `cmd/repoforge/main_test.go`, `scripts/release-check.sh`.
