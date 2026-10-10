# Testdata

Fixtures consumed by tests. Go tooling ignores directories named
`testdata`, so fixtures here never affect builds.

Rules:

- No credentials, tokens, or real repository secrets, ever.
- Fixtures must be minimal and deterministic.
- Golden files belong to the package that owns the test, under that
  package's own `testdata/`, unless shared across packages.
