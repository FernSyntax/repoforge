# Commit strategy

Conventional Commits, restricted subset.

## Format

```
<type>(<optional scope>)!: <subject>

<optional body>

<optional footers>
```

- Types: `feat`, `fix`, `docs`, `refactor`, `perf`, `test`, `build`,
  `ci`, `chore`, `revert`, `security`.
- Subject: imperative, no trailing period, 72 characters or fewer.
- Breaking changes: `!` after the type/scope **and** a
  `BREAKING CHANGE:` footer, plus a CHANGELOG.md entry.
- Reverts: `revert: <subject of reverted commit>`, body naming the
  reverted SHA and the reason.

## Quality rules

- **Atomic** — one logical change per commit.
- **Coherent** — code, tests, and docs for a change travel together;
  every commit builds and passes tests on its own.
- **Reviewable** — the diff can be understood in one sitting.
- **Reversible where practical** — irreversible steps are called out
  in the body.

## Why subjects matter more here

Squash merge is the only merge method. **The pull request title
becomes the commit subject on `main`**, so write PR titles as commit
subjects. Pre-merge commit noise on your branch is harmless; the
merged commit is the historical record.

## Never commit

Credentials, tokens, private keys, machine-specific absolute paths, or
other people's identities. Validation scans for these.
