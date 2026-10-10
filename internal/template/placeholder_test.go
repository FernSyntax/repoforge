package template

// ph renders a template placeholder at runtime rather than as a literal
// in this file's source, so no tracked file in the repository contains a
// literal double-brace variable syntax.
//
// The repository's own content validator (internal/validate) reports a
// literal double-brace syntax as an unresolved template placeholder. That is
// correct for the files listed in ownership.placeholder_paths — template
// sources and third-party templates such as GoReleaser's — and wrong for
// a Go test whose subject is the placeholder grammar itself. Widening the
// policy exemption to cover test fixtures would weaken the check for every
// file it covers, so the tests build the placeholder at runtime instead.
//
// template_test.go already does this with a function-local closure; this
// file promotes the helper to the package so the newer tests share it.
func ph(name string) string { return "{{" + " ." + name + " }}" }

// phTight renders the same placeholder without surrounding whitespace,
// which the grammar table in render_test.go exercises.
func phTight(name string) string { return "{{" + "." + name + "}}" }
