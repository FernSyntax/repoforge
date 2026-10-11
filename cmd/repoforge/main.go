// Command repoforge is the RepoForge CLI entry point.
//
// Implemented commands: validate (spec.md §12), doctor (spec.md §11), bootstrap
// (spec.md §10), template validate/build/adopt (spec.md §13), git commit-check
// and branch-check, and pr check. Exit codes: 0 = success, 1 = findings
// or pending changes, 2 = invocation or configuration error.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FernSyntax/repoforge/internal/bootstrap"
	"github.com/FernSyntax/repoforge/internal/doctor"
	ghub "github.com/FernSyntax/repoforge/internal/github"
	"github.com/FernSyntax/repoforge/internal/gitrules"
	"github.com/FernSyntax/repoforge/internal/schematools"
	"github.com/FernSyntax/repoforge/internal/template"
	"github.com/FernSyntax/repoforge/internal/validate"
	"github.com/FernSyntax/repoforge/internal/version"
	"github.com/FernSyntax/repoforge/internal/wiki"
)

// defaultTemplateSource is the canonical template source directory.
// It is the default for template-source commands (validate, build)
// only; template adopt targets a generated repository instead.
const defaultTemplateSource = "template"

// defaultSourceURL is the canonical engine repository recorded in a
// generated template manifest. It is a product constant, not a
// credential, and it is the only repository identity RepoForge needs to
// verify a published template.
const defaultSourceURL = "https://github.com/FernSyntax/repoforge"

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		usage(stderr)
		return 2
	}
	switch args[0] {
	case "validate":
		return runValidate(args[1:], stdout, stderr)
	case "doctor":
		return runDoctor(args[1:], stdout, stderr)
	case "bootstrap":
		return runBootstrap(args[1:], os.Stdin, stdout, stderr)
	case "template":
		return runTemplate(args[1:], stdout, stderr)
	case "git":
		return runGit(args[1:], stdout, stderr)
	case "wiki":
		return runWiki(args[1:], stdout, stderr)
	case "pr":
		return runPR(args[1:], stdout, stderr)
	case "release":
		return runRelease(args[1:], stdout, stderr)
	case "hook":
		return runHook(args[1:], stdout, stderr)
	case "issue":
		return runIssue(args[1:], stdout, stderr)
	case "schema":
		return runSchema(args[1:], stdout, stderr)
	case "version", "--version":
		fmt.Fprintln(stdout, version.Version)
		return 0
	case "help", "--help", "-h":
		usage(stdout)
		return 0
	default:
		fmt.Fprintf(stderr, "repoforge: unknown command %q\n\n", args[0])
		usage(stderr)
		return 2
	}
}

// runSchema checks the schema pairs. It carries no interpreter
// dependency: the parity check runs through internal/schematools, so the
// pipeline does not depend on PyYAML, which cannot be hash-pinned
// portably and is reported as a Pinned-Dependencies finding.
func runSchema(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "check" {
		fmt.Fprintln(stderr, "repoforge schema: want subcommand 'check'")
		return 2
	}
	fs := flag.NewFlagSet("schema check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "directory to start schema discovery from")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge schema check: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	root, err := schematools.RootFrom(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge schema check: %v\n", err)
		return 2
	}
	pairs := schematools.DefaultPairs()
	errs := schematools.CheckAll(pairs)
	for _, e := range errs {
		fmt.Fprintf(stderr, "repoforge schema check: %v\n", e)
	}
	if len(errs) > 0 {
		fmt.Fprintf(stdout, "schema check: %d of %d schema pair(s) are not identical\n", len(errs), len(pairs))
		return 1
	}
	for _, pair := range pairs {
		fmt.Fprintf(stdout, "OK %s == %s\n",
			filepath.Join(root, filepath.FromSlash(pair.YAML)),
			filepath.Join(root, filepath.FromSlash(pair.JSON)))
	}
	fmt.Fprintf(stdout, "schema check: %d schema pair(s) identical\n", len(pairs))
	return 0
}

func usage(w io.Writer) {
	fmt.Fprint(w, `repoforge - the RepoForge CLI

Usage:
  repoforge validate [--json] [--commits N] [--path DIR]
  repoforge doctor [--json] [--repo owner/name] [--path DIR]
  repoforge bootstrap [--dry-run] [--yes] [--json] [--repo owner/name] [--path DIR]
  repoforge template validate [--path template/]
  repoforge template build --out DIR [--path template/] [--var key=value ...] [--force]
  repoforge template verify [--path DIR] [--source-url URL]
  repoforge template provenance [--path DIR]
  repoforge template adopted [--path DIR]
  repoforge template adopt --var key=value ... [--path DIR] [--dry-run]
  repoforge git commit-check <message>
  repoforge git branch-check <branch>
  repoforge wiki sync [--path DIR] [--repo owner/name] [--apply] [--dry-run]
  repoforge pr check [--path DIR]
  repoforge pr scaffold --issue N
  repoforge pr ready --file BODY [--path DIR] [--repo owner/name]
  repoforge pr create --title T --file BODY [--path DIR] [--repo owner/name] [--base BRANCH] [--draft]
  repoforge release ready --tag vX.Y.Z [--path DIR]
  repoforge hook install|verify|uninstall [--path DIR]
  repoforge issue scaffold --type bug|feature|task|security [--path DIR]
  repoforge issue check --type bug|feature|task|security --file BODY [--path DIR]
  repoforge version

validate checks the local repository checkout against the RepoForge
standard. It is deterministic and offline (spec.md §12).

doctor audits local and remote (GitHub) state against the policy and
reports drift. It is strictly read-only (spec.md §11).

bootstrap converges GitHub state to the policy: plan, confirm, apply,
verify. It is idempotent. --dry-run prints the plan and changes nothing.

wiki sync converges the hosted GitHub wiki to the declared page set in
docs/wiki/. It is a dry run unless --apply is given: nothing is written
or pushed, and user-owned pages are never touched.

Exit codes: 0 = valid / converged, 1 = violations / drift / pending
changes, 2 = invocation or configuration error.
`)
}

// runWiki dispatches the wiki subcommands. sync is the only one; it is
// a dry run by default, and the result is reported the same way in both
// modes so a dry run is an exact preview of an apply.
func runWiki(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "sync" {
		fmt.Fprintln(stderr, "repoforge wiki: want subcommand 'sync'")
		return 2
	}
	fs := flag.NewFlagSet("wiki sync", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "directory to start repository discovery from")
	sourceDir := fs.String("source", wiki.DefaultSourceDir, "directory holding the declared wiki pages (relative to --path)")
	repoSlug := fs.String("repo", "", "repository slug owner/name (default: parse from origin remote)")
	apply := fs.Bool("apply", false, "write and push the pages; without it nothing is written")
	token := fs.String("token", "", "GitHub token for the wiki push (default: GH_TOKEN/GITHUB_TOKEN, else git's own credentials)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge wiki sync: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	vrep, err := validate.Run(validate.Options{StartDir: *startDir})
	if err != nil {
		fmt.Fprintf(stderr, "repoforge wiki sync: %v\n", err)
		return 2
	}
	slug := *repoSlug
	if slug == "" {
		slug, err = doctor.SlugFromOrigin(*startDir)
		if err != nil {
			fmt.Fprintf(stderr, "repoforge wiki sync: %v\n", err)
			return 2
		}
	}
	owner, repoName, ok := strings.Cut(strings.TrimSuffix(slug, ".git"), "/")
	if !ok || owner == "" || repoName == "" {
		fmt.Fprintf(stderr, "repoforge wiki sync: invalid repository slug %q (want owner/name)\n", slug)
		return 2
	}

	creds := *token
	if creds == "" {
		creds, err = ghub.ResolveToken(context.Background(), "")
		if err != nil && *apply {
			// Planning needs no credential, so an absent token is not a
			// usage error until the operator asks for a push.
			fmt.Fprintf(stderr, "repoforge wiki sync: --apply needs a token (set GH_TOKEN or GITHUB_TOKEN): %v\n", err)
			return 2
		}
	}

	res, err := wiki.Sync(context.Background(), wiki.Options{
		SourceDir: *sourceDir,
		Owner:     owner,
		Repo:      repoName,
		RepoRoot:  vrep.Root,
		Apply:     *apply,
		Token:     creds,
	})
	if err != nil {
		// Check the error before touching the result: Sync returns a
		// nil result when it could not read the wiki at all, and
		// printing the plan first is what turned a clean failure into a
		// panic. The plan is still printed when Sync produced one.
		if res != nil {
			printWikiPlan(stdout, res)
		}
		fmt.Fprintf(stderr, "repoforge wiki sync: %v\n", err)
		switch {
		case wiki.NotFound(err):
			return 1
		case wiki.Unauthorized(err):
			return 1
		}
		return 2
	}
	printWikiPlan(stdout, res)
	fmt.Fprintf(stdout, "wiki sync: %s — %d create, %d update, %d unchanged, %d user-owned\n",
		wikiTargetWord(res), res.Created, res.Updated, res.Unchanged, res.UserOwned)
	if res.Applied {
		fmt.Fprintf(stdout, "wiki sync: pushed %s to %s\n", res.Commit, res.PushedRef)
	}
	return 0
}

// printWikiPlan renders the plan in both modes, so a dry run is an exact
// preview of an apply.
func printWikiPlan(stdout io.Writer, res *wiki.Result) {
	for _, p := range res.Pages {
		fmt.Fprintf(stdout, "%-11s %-24s %s\n", p.Action, p.Name, p.Source)
	}
}

func wikiTargetWord(res *wiki.Result) string {
	if res.Applied {
		return "published to " + res.Repository
	}
	return "dry run (nothing written)"
}

func runValidate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("validate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit findings as JSON")
	commits := fs.Int("commits", 20, "number of recent commits to inspect for message policy")
	startDir := fs.String("path", ".", "directory to start repository discovery from")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge validate: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	rep, err := validate.Run(validate.Options{StartDir: *startDir, MaxCommits: *commits})
	if err != nil {
		fmt.Fprintf(stderr, "repoforge validate: %v\n", err)
		return 2
	}

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(rep); err != nil {
			fmt.Fprintf(stderr, "repoforge validate: cannot encode JSON: %v\n", err)
			return 2
		}
	} else {
		printTextReport(stdout, rep)
	}
	return validate.ExitCode(rep, nil)
}

func printTextReport(w io.Writer, rep validate.Report) {
	for _, f := range rep.Findings {
		fmt.Fprintf(w, "%s: [%s] %s\n", f.Path, f.Rule, f.Message)
	}
	if rep.Valid() {
		fmt.Fprintf(w, "validate: OK (%s)\n", rep.Root)
		return
	}
	fmt.Fprintf(w, "validate: %d violation(s) found\n", len(rep.Findings))
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("doctor", flag.ContinueOnError)
	fs.SetOutput(stderr)
	jsonOut := fs.Bool("json", false, "emit findings as JSON")
	repoSlug := fs.String("repo", "", "repository slug owner/name (default: parse from origin remote)")
	startDir := fs.String("path", ".", "directory to start repository discovery from")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge doctor: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	rep, err := doctor.Run(context.Background(), doctor.Options{StartDir: *startDir, Repo: *repoSlug})
	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(rep); encErr != nil {
			fmt.Fprintf(stderr, "repoforge doctor: cannot encode JSON: %v\n", encErr)
			return 2
		}
	} else {
		for _, f := range rep.Findings {
			fmt.Fprintf(stdout, "%-11s %s\n", f.Severity, f.Message)
		}
		pass, warn, fail, unsupported := rep.Summary()
		fmt.Fprintf(stdout, "doctor: %d pass, %d warn, %d fail, %d unsupported\n", pass, warn, fail, unsupported)
	}
	if err != nil {
		fmt.Fprintf(stderr, "repoforge doctor: %v\n", err)
		return 2
	}
	return doctor.ExitCode(rep, nil)
}

func runBootstrap(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dryRun := fs.Bool("dry-run", false, "print the plan; change nothing")
	yes := fs.Bool("yes", false, "apply without interactive confirmation")
	jsonOut := fs.Bool("json", false, "emit the plan and result as JSON")
	repoSlug := fs.String("repo", "", "repository slug owner/name (default: parse from origin remote)")
	startDir := fs.String("path", ".", "directory to start repository discovery from")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge bootstrap: unexpected argument %q\n", fs.Arg(0))
		return 2
	}

	opts := bootstrap.Options{StartDir: *startDir, Repo: *repoSlug, DryRun: *dryRun, Yes: *yes}
	if !*dryRun && !*yes {
		opts.Confirm = confirmPlan(stdin, stdout)
	}

	res, err := bootstrap.Run(context.Background(), opts)

	if *jsonOut {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if encErr := enc.Encode(res); encErr != nil {
			fmt.Fprintf(stderr, "repoforge bootstrap: cannot encode JSON: %v\n", encErr)
			return 2
		}
	} else {
		printPlan(stdout, res)
	}
	if err != nil {
		fmt.Fprintf(stderr, "repoforge bootstrap: %v\n", err)
		return 2
	}
	return bootstrap.ExitCode(res, nil)
}

// confirmPlan displays the plan and requires an explicit "yes" on an
// interactive terminal. Anything else — including a non-terminal stdin —
// declines, so bootstrap fails safe (spec.md P7).
func confirmPlan(stdin io.Reader, stdout io.Writer) func([]bootstrap.PlanItem) bool {
	return func(plan []bootstrap.PlanItem) bool {
		if f, ok := stdin.(*os.File); ok {
			if st, err := f.Stat(); err != nil || st.Mode()&os.ModeCharDevice == 0 {
				fmt.Fprintln(stdout, "bootstrap: no interactive terminal; declining to apply (use --yes to apply non-interactively)")
				return false
			}
		}
		fmt.Fprintf(stdout, "Apply %d change(s)? Type 'yes' to continue: ", countApplicable(plan))
		var answer string
		fmt.Fscanln(stdin, &answer)
		return answer == "yes"
	}
}

func countApplicable(plan []bootstrap.PlanItem) int {
	n := 0
	for _, p := range plan {
		if p.Action == bootstrap.ActionCreate || p.Action == bootstrap.ActionUpdate ||
			p.Action == bootstrap.ActionRename || p.Action == bootstrap.ActionEnable ||
			p.Action == bootstrap.ActionDisable {
			n++
		}
	}
	return n
}

func printPlan(w io.Writer, res bootstrap.Result) {
	for _, p := range res.Plan {
		fmt.Fprintf(w, "%-12s %-12s %s — %s\n", p.Area, "["+string(p.Action)+"]", p.Subject, p.Detail)
	}
	switch {
	case res.DryRun:
		fmt.Fprintf(w, "bootstrap: dry-run; %d change(s) would be applied\n", countApplicable(res.Plan))
	case res.Aborted:
		fmt.Fprintln(w, "bootstrap: aborted; nothing was applied")
	case res.Converged && res.Applied > 0:
		fmt.Fprintf(w, "bootstrap: applied %d change(s); converged\n", res.Applied)
	case res.Converged:
		fmt.Fprintln(w, "bootstrap: already converged; nothing to do")
	default:
		fmt.Fprintln(w, "bootstrap: NOT converged; review the plan above and re-run")
	}
}

type varFlags map[string]string

func (v varFlags) String() string { return fmt.Sprint(map[string]string(v)) }

func (v varFlags) Set(s string) error {
	k, val, ok := strings.Cut(s, "=")
	if !ok || k == "" {
		return fmt.Errorf("want key=value, got %q", s)
	}
	v[k] = val
	return nil
}

// runTemplate dispatches the template subcommands. Each subcommand
// resolves its own source directory, because "the default source" is
// different for each contract:
//
//   - validate and build read the canonical template source, which is
//     the template/ directory of this repository.
//   - adopt operates on a generated repository — the working directory
//     itself — and honours an explicit --path verbatim.
//
// Collapsing those into one shared default is what made the advertised
// `template adopt` command load a nonexistent template/ directory.
func runTemplate(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "repoforge template: want subcommand 'validate', 'build', 'verify', 'provenance', 'adopted', or 'adopt'")
		return 2
	}
	sub := args[0]

	srcDefault := defaultTemplateSource
	if sub == "adopt" || sub == "verify" {
		srcDefault = "."
	}

	fs := flag.NewFlagSet("template "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	srcDir := fs.String("path", srcDefault,
		"template source directory (validate, build) or the generated repository to check (verify, adopt)")
	outDir := fs.String("out", "", "output directory (build; required)")
	force := fs.Bool("force", false, "replace a non-empty existing output directory (build)")
	dryRun := fs.Bool("dry-run", false, "print planned substitutions without writing (adopt)")
	// Default empty: the manifest already records the URL it was built
	// from, and a build-time default here would silently override it.
	sourceURL := fs.String("source-url", "",
		"override the source URL recorded in the manifest (verification)")
	requireSource := fs.Bool("require-source", false,
		"verify: fail unless the manifest records an immutable RepoForge source commit (template CI)")
	sourceRef := fs.String("source-ref", "", "immutable RepoForge source reference (release tag) recorded in the manifest")
	sourceCommit := fs.String("source-commit", "", "full 40-character RepoForge source commit SHA recorded in the manifest")
	forgeVersion := fs.String("repoforge-version", version.Version, "RepoForge version recorded in the manifest")
	vars := varFlags{}
	fs.Var(vars, "var", "template variable key=value (repeatable)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}

	switch sub {
	case "validate":
		files, err := template.LoadSource(*srcDir)
		if err != nil {
			fmt.Fprintf(stderr, "repoforge template: %v\n", err)
			return 2
		}
		issues := template.ValidateSource(files)
		for _, i := range issues {
			fmt.Fprintln(stdout, i)
		}
		if len(issues) > 0 {
			fmt.Fprintf(stdout, "template validate: %d issue(s)\n", len(issues))
			return 1
		}
		fmt.Fprintf(stdout, "template validate: OK (%d files)\n", len(files))
		if findFile(files, template.ManifestFileName) != nil {
			fmt.Fprintln(stdout, "template validate: note — use `template verify` to prove reproducibility against the recorded source commit")
		}
		return 0
	case "build":
		if *outDir == "" {
			fmt.Fprintln(stderr, "repoforge template build: --out is required")
			return 2
		}
		files, err := template.LoadSource(*srcDir)
		if err != nil {
			fmt.Fprintf(stderr, "repoforge template: %v\n", err)
			return 2
		}
		// Variables and the destination are validated before anything is
		// deleted, and the tree is staged and published, so a rejected
		// destination is guaranteed untouched.
		issues, err := template.Build(files, *srcDir, *outDir, map[string]string(vars),
			template.BuildOptions{
				Force: *force,
				Manifest: template.ManifestOptions{
					RepoForgeVersion: *forgeVersion,
					SourceRef:        *sourceRef,
					SourceCommit:     *sourceCommit,
					SourceURL:        *sourceURL,
				},
			})
		for _, i := range issues {
			fmt.Fprintln(stdout, i)
		}
		if err != nil {
			fmt.Fprintf(stderr, "repoforge template build: %v\n", err)
			return 1
		}
		fmt.Fprintf(stdout, "template build: wrote %d files to %s\n", len(files), *outDir)
		return 0
	case "provenance":
		return runTemplateProvenance(*srcDir, stdout, stderr)
	case "adopted":
		return runTemplateAdopted(*srcDir, stdout, stderr)
	case "verify":
		issues, err := template.Verify(*srcDir, *sourceURL, *requireSource)
		for _, i := range issues {
			fmt.Fprintln(stdout, i)
		}
		if err != nil {
			fmt.Fprintf(stderr, "repoforge template verify: %v\n", err)
			return 2
		}
		if len(issues) > 0 {
			fmt.Fprintf(stderr, "repoforge template verify: %d difference(s); this tree is not a faithful build of the recorded source\n", len(issues))
			return 1
		}
		fmt.Fprintf(stdout, "template verify: %s matches the recorded RepoForge source\n", *srcDir)
		if !verifyIsTraceable(*srcDir) {
			fmt.Fprintln(stdout, "template verify: note — this is a development build with no source commit; pass --require-source to enforce provenance")
		}
		return 0
	case "adopt":
		return runTemplateAdopt(*srcDir, map[string]string(vars), *dryRun, stdout, stderr)
	default:
		fmt.Fprintf(stderr, "repoforge template: unknown subcommand %q\n", sub)
		return 2
	}
}

// runTemplateProvenance prints the build flags needed to reproduce the
// manifest recorded in dir. A determinism check can only compare like
// with like: rebuilding a provenance-bearing tree with a plain build
// produces a different manifest and therefore a false failure.
func runTemplateProvenance(dir string, stdout, stderr io.Writer) int {
	m, err := template.LoadManifest(dir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge template provenance: %v\n", err)
		return 2
	}
	if m.Adopted {
		fmt.Fprintln(stdout, "--adopted")
		return 0
	}
	// Only flags with a value are printed. A flag with an empty value
	// would shift the next flag into its position and silently corrupt
	// the rebuild, which is the whole failure this printer exists to
	// prevent.
	if m.SourceRef != "" {
		fmt.Fprintf(stdout, "--source-ref %s\n", m.SourceRef)
	}
	if m.SourceCommit != "" {
		fmt.Fprintf(stdout, "--source-commit %s\n", m.SourceCommit)
	}
	if m.SourceURL != "" {
		fmt.Fprintf(stdout, "--source-url %s\n", m.SourceURL)
	}
	if m.RepoForgeVersion != "" {
		fmt.Fprintf(stdout, "--repoforge-version %s\n", m.RepoForgeVersion)
	}
	return 0
}

// runTemplateAdopted reports whether dir is an adopted generated
// repository. It is the machine-readable form of "the provenance gate no
// longer applies here", and it is what a consumer project's CI uses to
// avoid failing for adding its own source.
func runTemplateAdopted(dir string, stdout, stderr io.Writer) int {
	m, err := template.LoadManifest(dir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge template adopted: %v\n", err)
		return 2
	}
	if !m.Adopted {
		fmt.Fprintf(stderr, "repoforge template adopted: %s is not an adopted generated repository\n", dir)
		return 1
	}
	fmt.Fprintf(stdout, "template adopted: %s is an adopted generated repository; the provenance gate does not apply\n", dir)
	return 0
}

// verifyIsTraceable reports whether the directory's manifest records
// an immutable source reference, so the note above is honest.
func verifyIsTraceable(dir string) bool {
	m, err := template.LoadManifest(dir)
	return err == nil && m.Verifiable()
}

// findFile returns the source file with the given repository-relative
// path, or nil.
func findFile(files []template.SourceFile, rel string) *template.SourceFile {
	for i := range files {
		if files[i].Rel == rel {
			return &files[i]
		}
	}
	return nil
}

// runTemplateAdopt resolves placeholders in place for an adopted
// generated repository. All known variables are required and every value
// is validated; the rendered result is checked against the
// generated-repository contract before anything is written; and the
// write is atomic with rollback, so a failure cannot leave the
// repository half-adopted.
func runTemplateAdopt(dir string, vars map[string]string, dryRun bool, stdout, stderr io.Writer) int {
	if err := template.AdoptableTarget(dir); err != nil {
		fmt.Fprintf(stderr, "repoforge template adopt: %v\n", err)
		return 2
	}
	files, err := template.LoadSource(dir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge template adopt: %v\n", err)
		return 2
	}
	rendered, report, err := template.Adopt(files, vars)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge template adopt: %v\n", err)
		return 2
	}
	changed := template.ChangedFiles(files, rendered)
	if len(changed) == 0 {
		fmt.Fprintln(stdout, "template adopt: nothing to do (already adopted with these values)")
		return 0
	}
	if issues := template.ValidateAdoption(changed); len(issues) > 0 {
		for _, i := range issues {
			fmt.Fprintln(stdout, i)
		}
		fmt.Fprintf(stderr, "repoforge template adopt: rendered output failed validation (%d issue(s)); nothing was written\n", len(issues))
		return 1
	}
	if dryRun {
		printAdoptReport(stdout, report, true)
		return 1
	}

	result, err := template.PublishAdoption(dir, files, rendered)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge template adopt: %v\n", err)
		return 1
	}
	printAdoptReport(stdout, report, false)
	fmt.Fprintf(stdout, "template adopt: wrote %d file(s)\n", len(result.Written))
	return 0
}

func printAdoptReport(w io.Writer, report []template.AdoptReport, planned bool) {
	verb := "substituted"
	if planned {
		verb = "would substitute"
	}
	total := 0
	for _, r := range report {
		var parts []string
		for _, k := range template.KnownVars {
			if n := r.Substitutions[k]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s×%d", k, n))
				total += n
			}
		}
		sort.Strings(parts)
		fmt.Fprintf(w, "%s: %s %s\n", r.Rel, verb, strings.Join(parts, ", "))
	}
	suffix := ""
	if planned {
		suffix = " planned"
	}
	fmt.Fprintf(w, "template adopt: %d file(s), %d substitution(s)%s\n", len(report), total, suffix)
}

func printIssues(w io.Writer, issues []gitrules.Issue) {
	for _, i := range issues {
		fmt.Fprintf(w, "%-6s %s: %s\n", i.Severity, i.Rule, i.Message)
	}
}

func runGit(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "repoforge git: want subcommand 'commit-check' or 'branch-check'")
		return 2
	}
	switch args[0] {
	case "commit-check":
		if len(args) < 2 {
			fmt.Fprintln(stderr, "repoforge git commit-check: message required")
			return 2
		}
		issues := gitrules.CheckCommitMessage(strings.Join(args[1:], " "))
		printIssues(stdout, issues)
		if gitrules.Failed(issues) {
			return 1
		}
		if len(issues) == 0 {
			fmt.Fprintln(stdout, "commit-check: OK")
		}
		return 0
	case "branch-check":
		if len(args) != 2 {
			fmt.Fprintln(stderr, "repoforge git branch-check: exactly one branch name required")
			return 2
		}
		issues := gitrules.CheckBranchName(args[1], "main")
		printIssues(stdout, issues)
		if gitrules.Failed(issues) {
			return 1
		}
		fmt.Fprintln(stdout, "branch-check: OK")
		return 0
	default:
		fmt.Fprintf(stderr, "repoforge git: unknown subcommand %q\n", args[0])
		return 2
	}
}

func runPR(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "check" && args[0] != "check-body" && args[0] != "scaffold" && args[0] != "ready" && args[0] != "create") {
		fmt.Fprintln(stderr, "repoforge pr: want subcommand 'check', 'check-body', 'scaffold', 'ready', or 'create'")
		return 2
	}
	// Each subcommand registers its own flags: a shared flag set here
	// would parse `--file` against the set that only knows `--path` and
	// reject it, which is exactly how the traceability check ended up
	// failing as a usage error on every pull request.
	switch args[0] {
	case "check":
		return runPRCheck(args[1:], stdout, stderr)
	case "scaffold":
		return runPRScaffold(args[1:], stdout, stderr)
	case "ready":
		return runPRReady(args[1:], stdout, stderr)
	case "create":
		return runPRCreate(args[1:], stdout, stderr)
	default:
		return runPRCheckBody(args[1:], stdout, stderr)
	}
}

// runPRScaffold prints the pull request body skeleton for an issue:
// every section the traceability contract requires, with the issue
// link pre-placed. The skeleton alone does not validate — fill every
// section, then prove it with `pr ready`.
func runPRScaffold(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr scaffold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	issue := fs.Int("issue", 0, "issue number the pull request closes")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge pr scaffold: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *issue <= 0 {
		fmt.Fprintln(stderr, "repoforge pr scaffold: --issue N is required (the issue the pull request closes)")
		return 2
	}
	fmt.Fprint(stdout, gitrules.PRScaffold(*issue))
	return 0
}

// runPRReady is the single local gate before opening a pull request:
// branch readiness plus the body contract. Exit 0 means every
// deterministic check CI runs will pass; anything else names exactly
// what is red. With --repo it also confirms the linked issue exists.
func runPRReady(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr ready", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "repository directory")
	bodyFile := fs.String("file", "", "file containing the pull request body; a value of - reads stdin")
	repoSlug := fs.String("repo", "", "repository slug owner/name; enables issue-existence validation")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge pr ready: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *bodyFile == "" {
		fmt.Fprintln(stderr, "repoforge pr ready: --file is required")
		return 2
	}
	data, err := readBody(*bodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge pr ready: %v\n", err)
		return 2
	}
	return prGate(*startDir, string(data), *repoSlug, stdout, stderr, "pr ready")
}

// prGate runs branch readiness plus the body contract and, with a
// repository slug, confirms the linked issue exists. It prints every
// finding and reports OK only when all three are green. Both `pr
// ready` and `pr create` run this; creation can only happen after it.
func prGate(startDir, body, repoSlug string, stdout, stderr io.Writer, name string) int {
	ready := gitrules.CheckPRReady(startDir, "main")
	printIssues(stdout, ready)
	contract := gitrules.CheckPRBody(body)
	printIssues(stdout, contract)
	if gitrules.Failed(ready) || gitrules.Failed(contract) {
		fmt.Fprintf(stdout, "%s: not ready\n", name)
		return 1
	}
	if repoSlug != "" {
		if err := confirmIssueExists(repoSlug, body); err != nil {
			fmt.Fprintf(stderr, "repoforge %s: %v\n", name, err)
			return 1
		}
		fmt.Fprintf(stdout, "%s: the linked issue exists\n", name)
	}
	fmt.Fprintf(stdout, "%s: OK\n", name)
	return 0
}

// runPRCreate gates exactly like `pr ready`, then delegates to
// `gh pr create`. The network call is textually and structurally
// after the gate: a red gate never reaches GitHub. Authentication
// stays where it belongs — `gh` owns it, and an unauthenticated `gh`
// fails with its own message, which is surfaced, not masked.
func runPRCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "repository directory")
	bodyFile := fs.String("file", "", "file containing the pull request body; a value of - reads stdin")
	title := fs.String("title", "", "pull request title (must follow commit conventions)")
	repoSlug := fs.String("repo", "", "repository slug owner/name; enables issue-existence validation")
	base := fs.String("base", "", "base branch (defaults to the repository default branch)")
	draft := fs.Bool("draft", false, "open the pull request as a draft")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge pr create: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *title == "" {
		fmt.Fprintln(stderr, "repoforge pr create: --title is required")
		return 2
	}
	if *bodyFile == "" {
		fmt.Fprintln(stderr, "repoforge pr create: --file is required")
		return 2
	}
	if issues := gitrules.CheckCommitMessage(*title); gitrules.Failed(issues) {
		printIssues(stdout, issues)
		fmt.Fprintln(stderr, "repoforge pr create: the title must follow commit conventions (it becomes the squash-merge subject)")
		return 1
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(stderr, "repoforge pr create: gh is not installed or not on PATH; install it first: https://cli.github.com")
		return 1
	}
	data, err := readBody(*bodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge pr create: %v\n", err)
		return 2
	}
	if code := prGate(*startDir, string(data), *repoSlug, stdout, stderr, "pr create"); code != 0 {
		return code
	}
	root, err := gitTopLevel(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge pr create: %v\n", err)
		return 1
	}
	ghArgs := []string{"pr", "create", "--title", *title, "--body-file", *bodyFile}
	if *base != "" {
		ghArgs = append(ghArgs, "--base", *base)
	}
	if *draft {
		ghArgs = append(ghArgs, "--draft")
	}
	return execGH(root, ghArgs, stdout, stderr)
}

// runRelease gates the release path. `release ready` runs the
// release-policy check as the pre-tag gate: shape, annotation,
// ancestry, changelog, version stamp. It performs no tagging,
// pushing, or publishing — the firing action stays human.
func runRelease(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || args[0] != "ready" {
		fmt.Fprintln(stderr, "repoforge release: want subcommand 'ready'")
		return 2
	}
	fs := flag.NewFlagSet("release ready", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "repository directory")
	tag := fs.String("tag", "", "release tag to validate (e.g. v0.1.1)")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge release ready: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *tag == "" {
		fmt.Fprintln(stderr, "repoforge release ready: --tag is required (e.g. --tag v0.1.1)")
		return 2
	}
	root, err := gitTopLevel(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge release ready: %v\n", err)
		return 1
	}
	script := filepath.Join(root, "scripts", "release-check.sh")
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(stderr, "repoforge release ready: cannot reach scripts/release-check.sh: %v\n", err)
		return 1
	}
	cmd := exec.Command("sh", script, *tag)
	cmd.Dir = root
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "repoforge release ready: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "release ready: OK")
	return 0
}

// runHook manages the developer-side Git hooks through
// scripts/hooks-install, discovered from the repository root so no
// script path has to be memorized. It installs nothing itself: the
// pre-commit framework still owns the blocking hooks, and hooks the
// installer does not own are still never replaced.
func runHook(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "install" && args[0] != "verify" && args[0] != "uninstall") {
		fmt.Fprintln(stderr, "repoforge hook: want subcommand 'install', 'verify', or 'uninstall'")
		return 2
	}
	fs := flag.NewFlagSet("hook "+args[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "repository directory")
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge hook %s: unexpected argument %q\n", args[0], fs.Arg(0))
		return 2
	}
	root, err := gitTopLevel(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge hook %s: %v\n", args[0], err)
		return 1
	}
	script := filepath.Join(root, "scripts", "hooks-install")
	if _, err := os.Stat(script); err != nil {
		fmt.Fprintf(stderr, "repoforge hook %s: cannot reach scripts/hooks-install: %v\n", args[0], err)
		return 1
	}
	scriptArgs := []string{}
	if args[0] == "verify" {
		scriptArgs = []string{"--verify"}
	} else if args[0] == "uninstall" {
		scriptArgs = []string{"--uninstall"}
	}
	cmd := exec.Command("sh", append([]string{script}, scriptArgs...)...)
	cmd.Dir = root
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "repoforge hook %s: %v\n", args[0], err)
		return 1
	}
	return 0
}

// gitTopLevel resolves the repository root for a directory, so
// wrappers operate on the checkout, not on the caller's cwd.
func gitTopLevel(dir string) (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("cannot find a git repository from %q", dir)
	}
	return strings.TrimSpace(string(out)), nil
}

// execGH delegates to the GitHub CLI with the caller's stdio. It is
// only ever reached after a green gate; its failures surface with
// gh's own message and exit code.
func execGH(dir string, ghArgs []string, stdout, stderr io.Writer) int {
	cmd := exec.Command("gh", ghArgs...)
	cmd.Dir = dir
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() != 0 {
			return ee.ExitCode()
		}
		fmt.Fprintf(stderr, "repoforge: gh failed: %v\n", err)
		return 1
	}
	return 0
}

// runIssue scaffolds and validates issue bodies against the
// repository's issue forms, so API and CLI authors file once and file
// right. No CI check constrains issue bodies; this pair is guidance
// with teeth, not a new contract.
func runIssue(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || (args[0] != "scaffold" && args[0] != "check" && args[0] != "create") {
		fmt.Fprintln(stderr, "repoforge issue: want subcommand 'scaffold', 'check', or 'create'")
		return 2
	}
	switch args[0] {
	case "scaffold":
		return runIssueScaffold(args[1:], stdout, stderr)
	case "check":
		return runIssueCheck(args[1:], stdout, stderr)
	default:
		return runIssueCreate(args[1:], stdout, stderr)
	}
}

// runIssueScaffold prints the skeleton for an issue form: one heading
// per required field. The skeleton alone does not validate — fill
// every field, then prove it with `issue check`.
func runIssueScaffold(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue scaffold", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "directory to start repository discovery from")
	typ := fs.String("type", "", "issue form: bug, feature, task, or security")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge issue scaffold: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *typ == "" {
		fmt.Fprintln(stderr, "repoforge issue scaffold: --type is required (bug, feature, task, or security)")
		return 2
	}
	root, err := gitrules.IssueFormRoot(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue scaffold: %v\n", err)
		return 1
	}
	_, fields, err := gitrules.LoadIssueFields(root, *typ)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue scaffold: %v\n", err)
		return 1
	}
	fmt.Fprint(stdout, gitrules.RenderIssueScaffold(fields))
	return 0
}

// runIssueCreate validates the drafted body against the form, then
// delegates to `gh issue create` with the form's declared labels, so
// automation files under the same labels a web-filed issue would get.
// Gate-before-delegate, as in `pr create`.
func runIssueCreate(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue create", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "directory to start repository discovery from")
	typ := fs.String("type", "", "issue form: bug, feature, task, or security")
	title := fs.String("title", "", "issue title")
	bodyFile := fs.String("file", "", "file containing the issue body; a value of - reads stdin")
	repoSlug := fs.String("repo", "", "repository slug owner/name; creation defaults to the checkout's repository")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge issue create: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *typ == "" {
		fmt.Fprintln(stderr, "repoforge issue create: --type is required (bug, feature, task, or security)")
		return 2
	}
	if *title == "" {
		fmt.Fprintln(stderr, "repoforge issue create: --title is required")
		return 2
	}
	if *bodyFile == "" {
		fmt.Fprintln(stderr, "repoforge issue create: --file is required")
		return 2
	}
	if _, err := exec.LookPath("gh"); err != nil {
		fmt.Fprintln(stderr, "repoforge issue create: gh is not installed or not on PATH; install it first: https://cli.github.com")
		return 1
	}
	data, err := readBody(*bodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue create: %v\n", err)
		return 2
	}
	root, err := gitrules.IssueFormRoot(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue create: %v\n", err)
		return 1
	}
	_, fields, err := gitrules.LoadIssueFields(root, *typ)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue create: %v\n", err)
		return 1
	}
	issues := gitrules.CheckIssueBody(fields, string(data))
	printIssues(stdout, issues)
	if gitrules.Failed(issues) {
		fmt.Fprintln(stdout, "issue create: not ready")
		return 1
	}
	labels, err := gitrules.IssueFormLabels(root, *typ)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue create: %v\n", err)
		return 1
	}
	ghArgs := []string{"issue", "create", "--title", *title, "--body-file", *bodyFile}
	for _, l := range labels {
		ghArgs = append(ghArgs, "--label", l)
	}
	if *repoSlug != "" {
		ghArgs = append(ghArgs, "--repo", *repoSlug)
	}
	top, err := gitTopLevel(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue create: %v\n", err)
		return 1
	}
	return execGH(top, ghArgs, stdout, stderr)
}

// runIssueCheck validates a drafted issue body against the form's
// required fields.
func runIssueCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("issue check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "directory to start repository discovery from")
	typ := fs.String("type", "", "issue form: bug, feature, task, or security")
	bodyFile := fs.String("file", "", "file containing the issue body; a value of - reads stdin")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge issue check: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	if *typ == "" {
		fmt.Fprintln(stderr, "repoforge issue check: --type is required (bug, feature, task, or security)")
		return 2
	}
	if *bodyFile == "" {
		fmt.Fprintln(stderr, "repoforge issue check: --file is required")
		return 2
	}
	data, err := readBody(*bodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue check: %v\n", err)
		return 2
	}
	root, err := gitrules.IssueFormRoot(*startDir)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue check: %v\n", err)
		return 1
	}
	_, fields, err := gitrules.LoadIssueFields(root, *typ)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge issue check: %v\n", err)
		return 1
	}
	issues := gitrules.CheckIssueBody(fields, string(data))
	printIssues(stdout, issues)
	if gitrules.Failed(issues) {
		fmt.Fprintln(stdout, "issue check: not ready")
		return 1
	}
	fmt.Fprintln(stdout, "issue check: OK")
	return 0
}

// runPRCheck reports whether the branch is ready for a pull request.
func runPRCheck(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr check", flag.ContinueOnError)
	fs.SetOutput(stderr)
	startDir := fs.String("path", ".", "repository directory")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() != 0 {
		fmt.Fprintf(stderr, "repoforge pr check: unexpected argument %q\n", fs.Arg(0))
		return 2
	}
	issues := gitrules.CheckPRReady(*startDir, "main")
	printIssues(stdout, issues)
	if gitrules.Failed(issues) {
		fmt.Fprintln(stdout, "pr check: not ready")
		return 1
	}
	fmt.Fprintln(stdout, "pr check: OK")
	return 0
}

// runPRCheckBody validates a pull request body against the traceability
// contract (spec.md §20.2). With --repo and a resolvable credential it
// also confirms the referenced issue exists, so a typo'd or deleted
// issue fails the check instead of sailing through.
func runPRCheckBody(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("pr check-body", flag.ContinueOnError)
	fs.SetOutput(stderr)
	bodyFile := fs.String("file", "", "file containing the pull request body; a value of - reads stdin")
	repoSlug := fs.String("repo", "", "repository slug owner/name; enables issue-existence validation")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *bodyFile == "" {
		fmt.Fprintln(stderr, "repoforge pr check-body: --file is required")
		return 2
	}
	data, err := readBody(*bodyFile)
	if err != nil {
		fmt.Fprintf(stderr, "repoforge pr check-body: %v\n", err)
		return 2
	}

	issues := gitrules.CheckPRBody(string(data))
	printIssues(stdout, issues)
	if gitrules.Failed(issues) {
		fmt.Fprintln(stdout, "pr check-body: not ready")
		return 1
	}

	if *repoSlug != "" {
		if err := confirmIssueExists(*repoSlug, string(data)); err != nil {
			fmt.Fprintf(stderr, "repoforge pr check-body: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, "pr check-body: the linked issue exists")
	}
	fmt.Fprintln(stdout, "pr check-body: OK")
	return 0
}

// readBody reads a pull request body from a file or stdin.
func readBody(name string) ([]byte, error) {
	if name == "-" {
		return io.ReadAll(os.Stdin)
	}
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, fmt.Errorf("reading the pull request body: %w", err)
	}
	return data, nil
}

// confirmIssueExists resolves the linked issue against the GitHub API.
// A missing credential is reported rather than silently skipped, unless
// the caller did not ask for remote validation at all.
func confirmIssueExists(slug, body string) error {
	number, ok := gitrules.IssueReference(body)
	if !ok {
		// Already reported by CheckPRBody; nothing more to confirm.
		return nil
	}
	owner, repo, ok := strings.Cut(strings.TrimSuffix(slug, ".git"), "/")
	if !ok || owner == "" || repo == "" {
		return fmt.Errorf("invalid repository slug %q (want owner/name)", slug)
	}
	ctx := context.Background()
	token, err := ghub.ResolveToken(ctx, "")
	if err != nil {
		return fmt.Errorf("cannot verify issue #%d without a credential: %w", number, err)
	}
	client := ghub.New(ghub.WithToken(token))
	return client.IssueExists(ctx, owner, repo, number)
}
