// Package provider is the engine's inference boundary: a central,
// instance-based client over the OpenAI-compatible HTTP contract the
// FreeLLMAPI gateway documents (ADR-0016, spec.md §24 FR-032–FR-034,
// FR-038).
//
// Application code routes logical tasks to model routes through Routes;
// no model ID is hardcoded outside configuration. Capabilities are
// discovered at runtime (ListReady) rather than assumed: there is no
// static capability matrix to go stale. Failover stays within the
// configured free routes and pauses on exhaustion; paid fallback does
// not exist in this package. Credentials come from the environment at
// call time and are redacted from every error.
package provider
