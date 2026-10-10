# Operations

What an on-call engineer needs. The normative operational contract lives
in `spec.md` §10.

## Release

| Step | Command or action |
| --- | --- |
| 1 | Confirm the tag is strict `vMAJOR.MINOR.PATCH` and annotated. |
| 2 | Confirm `CHANGELOG.md` has a section for the tag. |
| 3 | Push the tag; release automation builds and publishes. |
| 4 | Verify the release assets: checksums, SBOM, provenance. |

```sh
<<RELEASE VERIFICATION COMMAND>>
```

## Rollback

1. Re-deploy the previous known-good artifact.
2. Confirm the health checks pass.
3. Open an issue describing the regression and link the failed release.

## Health and diagnostics

| Signal | Command | Healthy value |
| --- | --- | --- |
| <<SIGNAL>> | `<<COMMAND>>` | <<HEALTHY VALUE>> |

## Common failures

| Symptom | Likely cause | First action |
| --- | --- | --- |
| <<SYMPTOM>> | <<CAUSE>> | <<FIRST ACTION>> |

## Escalation

<<WHO TO CONTACT, HOW, AND WHAT INFORMATION TO BRING>>
