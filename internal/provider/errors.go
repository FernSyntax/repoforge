package provider

import (
	"strconv"
	"time"
)

// Kind classifies a provider failure so callers can decide bounded
// retries, pause-and-resume, or fail-fast without parsing messages.
type Kind string

// Failure kinds. Only rate-limit, server, network, and timeout failures
// are retryable; everything else fails fast.
const (
	// KindInvalidConfig means the client is unusable (missing key or
	// base URL). No request is made.
	KindInvalidConfig Kind = "invalid-config"
	// KindAuth means the gateway rejected the credential (401/403).
	KindAuth Kind = "auth"
	// KindInvalidRequest means the request itself is wrong
	// (400/404/422, for example an unknown profile or an
	// unsatisfiable capability demand).
	KindInvalidRequest Kind = "invalid-request"
	// KindRateLimit means the gateway answered 429: free capacity is
	// exhausted for now. The caller pauses; it never invents quota.
	KindRateLimit Kind = "rate-limit"
	// KindServer means the gateway or an upstream failed (5xx).
	KindServer Kind = "server"
	// KindNetwork means the request never completed at the transport
	// layer.
	KindNetwork Kind = "network"
	// KindTimeout means the attempt exceeded its deadline.
	KindTimeout Kind = "timeout"
	// KindInvalidOutput means a 2xx response did not match the
	// chat-completions shape (unparseable or wrong schema).
	KindInvalidOutput Kind = "invalid-output"
	// KindEmptyResponse means a well-formed 2xx carried no usable
	// content.
	KindEmptyResponse Kind = "empty-response"
	// KindNoEligibleRoute means no model route is configured for the
	// task. Paid fallback does not exist.
	KindNoEligibleRoute Kind = "no-eligible-route"
)

// Retryable reports whether the kind may be attempted again under the
// attempt bound. Deterministic client errors never retry.
func (k Kind) Retryable() bool {
	switch k {
	case KindRateLimit, KindServer, KindNetwork, KindTimeout:
		return true
	default:
		return false
	}
}

// RequestError is a classified provider failure. It never contains the
// credential: response bodies are redacted before being embedded.
type RequestError struct {
	Kind       Kind
	StatusCode int
	Message    string
	// RetryAfter carries the gateway's Retry-After hint, if any.
	RetryAfter time.Duration
	// Attempts is the total number of attempts made for the request.
	Attempts int
}

func (e *RequestError) Error() string {
	if e.Attempts > 1 {
		return "provider: " + string(e.Kind) + " (attempts " + strconv.Itoa(e.Attempts) + "): " + e.Message
	}
	return "provider: " + string(e.Kind) + ": " + e.Message
}
