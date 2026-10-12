// Copyright the repoforge contributors.
//
// SPDX-License-Identifier: Apache-2.0

package doctor

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/FernSyntax/repoforge/internal/config"
	"github.com/FernSyntax/repoforge/internal/validate"
)

// runLocal performs the local (offline) audit checks by delegating to
// the deterministic validator and mapping its findings to severities.
// Returns the repository root and the loaded policy (nil when absent
// or invalid).
func runLocal(rep *Report, startDir string) (string, *config.Policy, error) {
	if startDir == "" {
		startDir = "."
	}
	abs, err := filepath.Abs(startDir)
	if err != nil {
		return "", nil, fmt.Errorf("doctor: %v", err)
	}

	vrep, err := validate.Run(validate.Options{StartDir: abs})
	if err != nil {
		return "", nil, fmt.Errorf("doctor: %v", err)
	}
	rep.add(Pass, "local/git", "git repository found (%s)", vrep.Root)

	var pol *config.Policy
	if vrep.Policy != "" {
		p, _, loadErr := config.LoadPolicy(vrep.Policy)
		if loadErr == nil {
			pol = p
		}
	}

	mapped := map[string][]string{}
	for _, f := range vrep.Findings {
		mapped[f.Rule] = append(mapped[f.Rule], fmt.Sprintf("%s: %s", f.Path, f.Message))
	}

	// configuration
	switch {
	case len(mapped["config/missing"]) > 0:
		rep.add(Fail, "local/config", "policy file missing (repoforge.yaml)")
	case len(mapped["config/schema"]) > 0:
		for _, m := range mapped["config/schema"] {
			rep.add(Fail, "local/config", "%s", m)
		}
	default:
		rep.add(Pass, "local/config", "policy %s is valid", filepath.Base(vrep.Policy))
	}

	// standard version
	if pol != nil {
		rep.add(Pass, "local/standard-version", "standard version %s is current", pol.Standard.Version)
	}

	// required files
	if msgs := mapped["files/required"]; len(msgs) > 0 {
		for _, m := range msgs {
			rep.add(Fail, "local/required-files", "%s", m)
		}
	} else {
		rep.add(Pass, "local/required-files", "required baseline files present")
	}

	// ownership model
	if msgs := mapped["files/ownership"]; len(msgs) > 0 {
		for _, m := range msgs {
			rep.add(Fail, "local/ownership", "%s", m)
		}
	} else {
		rep.add(Pass, "local/ownership", "ownership declarations consistent")
	}

	// placeholders
	if msgs := mapped["content/placeholder"]; len(msgs) > 0 {
		for _, m := range msgs {
			rep.add(Fail, "local/placeholders", "%s", m)
		}
	} else {
		rep.add(Pass, "local/placeholders", "no unresolved template placeholders")
	}

	return vrep.Root, pol, nil
}

// SlugFromOrigin reads the origin remote URL (read-only) and parses an
// owner/name slug from SSH or HTTPS GitHub URLs.
func SlugFromOrigin(startDir string) (string, error) {
	abs, _ := filepath.Abs(startDir)
	cmd := exec.Command("git", "remote", "get-url", "origin")
	cmd.Dir = abs
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("doctor: cannot determine repository slug (no origin remote); pass --repo owner/name")
	}
	url := strings.TrimSpace(string(out))
	url = strings.TrimSuffix(url, ".git")
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		return strings.TrimPrefix(url, "git@github.com:"), nil
	case strings.HasPrefix(url, "https://github.com/"):
		return strings.TrimPrefix(url, "https://github.com/"), nil
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		return strings.TrimPrefix(url, "ssh://git@github.com/"), nil
	}
	return "", fmt.Errorf("doctor: origin URL %q is not a recognized GitHub URL; pass --repo owner/name", url)
}
