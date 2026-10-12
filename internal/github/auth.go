// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package github

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ErrNoCredentials is returned when no credential source yields a token.
var ErrNoCredentials = errors.New("github: no credentials found")

// ResolveToken resolves a credential in the documented order (spec.md §8.1):
// explicit argument, GH_TOKEN, GITHUB_TOKEN, then the authenticated
// GitHub CLI session. The token is never written to disk and never
// printed; the returned error names only the sources tried.
func ResolveToken(ctx context.Context, explicit string) (string, error) {
	if explicit != "" {
		return explicit, nil
	}
	if t := os.Getenv("GH_TOKEN"); t != "" {
		return t, nil
	}
	if t := os.Getenv("GITHUB_TOKEN"); t != "" {
		return t, nil
	}
	if path, err := exec.LookPath("gh"); err == nil {
		ghCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ghCtx, path, "auth", "token").Output()
		if err == nil {
			if t := strings.TrimSpace(string(out)); t != "" {
				return t, nil
			}
		}
	}
	return "", fmt.Errorf("%w: tried explicit token, GH_TOKEN, GITHUB_TOKEN, and 'gh auth token'; authenticate with one of them (never paste tokens into files)", ErrNoCredentials)
}
