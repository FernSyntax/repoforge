package template

import (
	"fmt"

	"github.com/FernSyntax/repoforge/internal/version"
)

// BuildProduct renders a product template source (for example
// product/astro) into a destination directory. It reuses the same
// primitives as the governance build — variable validation,
// rendering, manifest provenance, output policy, and atomic publish —
// but replaces the governance-baseline checks with an
// unresolved-placeholder gate: every placeholder the source declares
// must be supplied, so generated output never ships template syntax.
//
// Safety contract: identical to Build (validated variables, resolved
// destination, validated output before anything is deleted, temporary
// directory published by rename). A non-empty pre-existing
// destination is refused unless Force is set.
func BuildProduct(files []SourceFile, srcDir, outDir string, vars map[string]string, opts BuildOptions) ([]Issue, error) {
	if err := ValidateVars(vars); err != nil {
		return nil, err
	}
	dest, err := OutputPolicy{SourceDir: srcDir, Force: opts.Force}.Resolve(outDir)
	if err != nil {
		return nil, err
	}

	rendered := RenderFiles(files, vars)
	if issues := CheckNoUnresolvedPlaceholders(rendered); len(issues) != 0 {
		return issues, fmt.Errorf("template: product source has placeholders with no value (%d issue(s)); %s was not modified", len(issues), dest)
	}
	manifest := BuildManifest(rendered, ManifestOptions{
		RepoForgeVersion: version.Version,
		SourceRef:        opts.Manifest.SourceRef,
		SourceCommit:     opts.Manifest.SourceCommit,
		SourceURL:        opts.Manifest.SourceURL,
	})
	rendered = append(rendered, SourceFile{Rel: ManifestFileName, Data: nil, Mode: 0o644})
	if err := checkDestination(dest, opts.Force); err != nil {
		return nil, err
	}
	if err := publishTree(dest, rendered, manifest, CheckNoUnresolvedPlaceholders); err != nil {
		return nil, err
	}
	return nil, nil
}
