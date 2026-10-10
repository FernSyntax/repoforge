# Incident Response

What to do when something has already gone wrong. Stop, preserve
evidence, then act — in that order.

## A credential may have leaked

1. **Revoke first, investigate second.** For a GitHub App installation
   token: App page → Advanced → Revoke all installation tokens. For a
   PAT: revoke it in the user's settings.
2. **Rotate.** Generate a new key, update the secret, delete the old
   one. See
   [template-publication.md](template-publication.md#rotating-or-revoking).
3. **Then** check whether the credential appeared anywhere committed:
   `git log -p` and the release artifacts. If it did, the commit history
   contains it and rotation is not optional.
4. Record the incident as an issue (private details in the security
   advisory, never in the public issue body).

## A release published something wrong

A release is never edited in place.

1. Confirm what actually shipped: `sha256sum -c checksums.txt`, then
   `gh attestation verify`.
2. If the artifacts are wrong, the release is not usable. Publish a new
   patch release; do not move or delete the tag (spec.md §14.6).
3. If the tag itself is wrong, leave it and publish the corrected
   version under a new tag. A moved tag destroys the provenance chain
   the checksums depend on.

## The template repository diverged

1. Confirm the divergence: `repoforge template verify --path <clone>`.
2. If the divergence is a hand edit, restore the canonical state by
   running the routine publication for the recorded source version —
   it will produce a generated pull request that converges the tree.
3. If the divergence is intentional, fold it back into the engine's
   `template/` source and publish a new version. A template change that
   exists only in the template repository is a change that will be lost.

## A bootstrap mutated something it should not

1. **Stop.** Do not re-run bootstrap: the second run converges to the
   same wrong state.
2. Restore the affected files from version control.
3. For remote settings, use `repoforge doctor` to read the current
   state, then plan the correction by hand.
4. Open a security-severity issue (privately if a permission boundary
   was crossed) with the plan output attached. The plan is the audit
   trail.
5. Add a regression test that reproduces the mutation before fixing it.

## A required check is failing and the change is urgent

The answer is never to disable the check.

1. Fix the underlying problem, or narrow the check if the check is
   wrong — with a test.
2. If the fix genuinely cannot land before the risk is unacceptable,
   the urgent path is a small, reviewed pull request that states
   `urgent-security-fix` and is merged by a maintainer. It is visible in
   the record, and it is followed by the real fix.
3. Record the exception as an issue so the follow-up is tracked.

## Escalation

| Severity | Response | Who |
| --- | --- | --- |
| Security boundary crossed | Immediate; private advisory | Maintainers |
| Release integrity broken | Same day; new patch release | Release owner |
| Template diverged | Next routine publication | Maintainer |
| Documentation drift | Normal issue-first flow | Anyone |
