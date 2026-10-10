package bootstrap

import (
	"context"
	"fmt"

	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// execEnv carries the client and repository identity that plan item
// closures use when (and only when) applied.
type execEnv struct {
	client *ghub.Client
	owner  string
	repo   string
}

// applyPlan executes applicable plan items in order, failing safely:
// the first error stops application, and the number of items already
// applied is reported so the user knows the partial state (spec.md §10.6).
// Items with no mutation (no-op/report/conflict/unsupported) are
// skipped by construction.
func applyPlan(ctx context.Context, plan []PlanItem) (int, error) {
	applied := 0
	for _, item := range plan {
		if !item.applicable() {
			continue
		}
		if err := item.apply(ctx); err != nil {
			return applied, fmt.Errorf("bootstrap: applying %s/%s after %d change(s): %w (re-run bootstrap to resume; it is idempotent)", item.Area, item.Subject, applied, err)
		}
		applied++
	}
	return applied, nil
}
