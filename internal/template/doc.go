// Package template builds the generated repository baseline consumed by
// the RepoForge template repository. Generated templates must never
// contain RepoForge implementation source code (spec.md §13.3).
//
// Three validation boundaries exist on purpose. ValidateSource checks the
// canonical template source (engine leakage, placeholders, required
// files). ValidateGenerated checks a rendered output or an existing
// generated repository, where Go source and a go.mod are the
// developer's own content and must not be reported as leaks.
// ValidateBuiltTree adds the manifest rules that only a built tree can
// satisfy. Verify goes further and rebuilds from the source commit the
// manifest records, so a hand-edited tree cannot pass by regenerating a
// matching manifest.
//
// Generation is staged in a temporary directory and published by rename;
// a destination is accepted only after every variable, value and option
// has been validated and the output-path policy has accepted it. Every
// build writes repoforge.manifest.json, whose digest never covers
// itself. See spec.md, "Template model".
package template
