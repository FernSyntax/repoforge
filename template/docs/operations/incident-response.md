# Incident Response

Stop, preserve evidence, then act — in that order.

## A credential may have leaked

1. Revoke it first, investigate second.
2. Rotate it.
3. Check whether it appears in any committed file or artifact; if it
   does, rotation is not optional.
4. Report privately per [SECURITY.md](../../SECURITY.md).

## A release shipped something wrong

A release is never edited in place. Verify what shipped
(`sha256sum -c checksums.txt`, then `gh attestation verify`) and publish
a corrected version under a new tag. A moved tag destroys the
provenance chain the checksums depend on.

## A required check is failing and the change is urgent

The answer is never to disable the check.

1. Fix the problem, or narrow the check if the check is wrong — with a
   test.
2. If it genuinely cannot wait, land a small, reviewed pull request that
   states `urgent-security-fix`, and follow it with the real fix.
3. Record the exception as an issue so the follow-up is tracked.

## Something mutated state it should not

1. Stop. Do not re-run the tool: a second run converges to the same
   wrong state.
2. Restore affected files from version control.
3. Open an issue with the plan output attached — the plan is the audit
   trail.
4. Add a regression test that reproduces the mutation before fixing it.
