# Architecture

A summary of the system shape. The normative description lives in
[`spec.md`](https://github.com/<<OWNER>>/<<REPOSITORY>>/blob/main/spec.md)
§7.

## Components

| Component | Responsibility | Owns |
| --- | --- | --- |
| <<COMPONENT>> | <<WHAT IT DOES>> | <<DATA OR INTERFACE>> |

## Diagram

```
<<COMPONENT A>> --> <<COMPONENT B>> --> <<DATA STORE>>
```

## Trust boundaries

| Boundary | What crosses it | What is allowed |
| --- | --- | --- |
| <<BOUNDARY>> | <<WHAT CROSSES>> | <<POLICY>> |

## Key dependencies

| Dependency | Purpose | Failure behaviour |
| --- | --- | --- |
| <<DEPENDENCY>> | <<WHY>> | <<WHAT THE SYSTEM DOES>> |

## Reading order for a new engineer

1. `spec.md` — what the system must do.
2. `docs/decisions/` — the choices that shaped it and the alternatives
   that were rejected.
3. The entry point of each component in `src/`.
