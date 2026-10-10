package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/FernSyntax/repoforge/configs"
	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// stateful fake GitHub --------------------------------------------------

type fakeRuleset struct {
	id          int
	name        string
	target      string
	sourceType  string
	enforcement string
	rules       []map[string]any
	// conditions and bypassActors round-trip the create/update body so
	// a ruleset created by bootstrap reads back as conforming.
	conditions   map[string]any
	bypassActors []map[string]any
}

// fakeLabel is one repository label. color/description round-trip the
// create/update body so a label written by bootstrap reads back as
// conforming.
type fakeLabel struct {
	name        string
	color       string
	description string
}

type fakeGitHub struct {
	mu                   sync.Mutex
	visibility           string
	defaultBranch        string
	features             map[string]bool
	merge                map[string]bool
	topics               []string
	languages            map[string]int
	rulesets             map[int]*fakeRuleset
	nextRulesetID        int
	labels               map[string]*fakeLabel
	vulnAlerts           bool
	autoFixes            bool
	privateVulnReporting bool
	secretScanning       string
	pushProtection       string
	codeScanningSetup    string
	requests             []string
	patchBodies          []string
	server               *httptest.Server
}

func newFakeGitHub(t *testing.T) *fakeGitHub {
	t.Helper()
	f := &fakeGitHub{
		visibility:    "public",
		defaultBranch: "main",
		features: map[string]bool{
			"has_issues": true, "has_discussions": false, "has_projects": true, "has_wiki": true,
		},
		merge: map[string]bool{
			"allow_squash_merge": true, "allow_merge_commit": true, "allow_rebase_merge": true,
			"delete_branch_on_merge": false, "allow_auto_merge": false,
		},
		languages:      map[string]int{"Go": 1000},
		rulesets:       map[int]*fakeRuleset{},
		nextRulesetID:  100,
		labels:         map[string]*fakeLabel{},
		secretScanning: "disabled",
		pushProtection: "disabled",
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/repos/o/r", f.handleRepo)
	mux.HandleFunc("/repos/o/r/topics", f.handleTopics)
	mux.HandleFunc("/repos/o/r/rulesets", f.handleRulesets)
	mux.HandleFunc("/repos/o/r/rulesets/", f.handleRulesetByID)
	mux.HandleFunc("/repos/o/r/labels", f.handleLabels)
	mux.HandleFunc("/repos/o/r/labels/", f.handleLabelByName)
	mux.HandleFunc("/repos/o/r/vulnerability-alerts", f.handleVulnAlerts)
	mux.HandleFunc("/repos/o/r/automated-security-fixes", f.handleAutoFixes)
	mux.HandleFunc("/repos/o/r/private-vulnerability-reporting", f.handlePrivateVulnReporting)
	mux.HandleFunc("/repos/o/r/code-scanning/default-setup", f.handleCodeScanning)
	mux.HandleFunc("/repos/o/r/languages", f.handleLanguages)
	mux.HandleFunc("/repos/o/r/branches/", f.handleBranchRename)
	f.server = httptest.NewServer(mux)
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeGitHub) log(r *http.Request, body []byte) {
	f.requests = append(f.requests, r.Method+" "+r.URL.Path)
	if r.Method == http.MethodPatch && len(body) > 0 {
		f.patchBodies = append(f.patchBodies, string(body))
	}
}

func (f *fakeGitHub) repoJSON() []byte {
	f.mu.Lock()
	defer f.mu.Unlock()
	obj := map[string]any{
		"full_name": "o/r", "visibility": f.visibility, "default_branch": f.defaultBranch,
		"security_and_analysis": map[string]any{
			"secret_scanning":                 map[string]any{"status": f.secretScanning},
			"secret_scanning_push_protection": map[string]any{"status": f.pushProtection},
		},
	}
	for k, v := range f.features {
		obj[k] = v
	}
	for k, v := range f.merge {
		obj[k] = v
	}
	out, _ := json.Marshal(obj)
	return out
}

func (f *fakeGitHub) handleRepo(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	switch r.Method {
	case http.MethodGet:
		w.Write(f.repoJSONLockedHelper())
	case http.MethodPatch:
		var patch map[string]json.RawMessage
		body := readBody(r)
		f.log(r, body)
		json.Unmarshal(body, &patch)
		for k, raw := range patch {
			if k == "security_and_analysis" {
				var sa map[string]map[string]string
				json.Unmarshal(raw, &sa)
				if v, ok := sa["secret_scanning"]; ok {
					f.secretScanning = v["status"]
				}
				if v, ok := sa["secret_scanning_push_protection"]; ok {
					f.pushProtection = v["status"]
				}
				continue
			}
			var b bool
			if json.Unmarshal(raw, &b) == nil {
				if _, ok := f.features[k]; ok {
					f.features[k] = b
				}
				if _, ok := f.merge[k]; ok {
					f.merge[k] = b
				}
			}
		}
		w.Write(f.repoJSONLockedHelper())
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) repoJSONLockedHelper() []byte {
	obj := map[string]any{
		"full_name": "o/r", "visibility": f.visibility, "default_branch": f.defaultBranch,
		"security_and_analysis": map[string]any{
			"secret_scanning":                 map[string]any{"status": f.secretScanning},
			"secret_scanning_push_protection": map[string]any{"status": f.pushProtection},
		},
	}
	for k, v := range f.features {
		obj[k] = v
	}
	for k, v := range f.merge {
		obj[k] = v
	}
	out, _ := json.Marshal(obj)
	return out
}

func (f *fakeGitHub) handleTopics(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	switch r.Method {
	case http.MethodGet:
		fmt.Fprintf(w, `{"names": %s}`, mustJSON(f.topics))
	case http.MethodPut:
		var body struct {
			Names []string `json:"names"`
		}
		json.Unmarshal(readBody(r), &body)
		f.topics = body.Names
		fmt.Fprintf(w, `{"names": %s}`, mustJSON(f.topics))
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleRulesets(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	switch r.Method {
	case http.MethodGet:
		var list []map[string]any
		for _, rs := range f.rulesets {
			list = append(list, map[string]any{
				"id": rs.id, "name": rs.name, "target": rs.target,
				"source_type": rs.sourceType, "source": "o/r", "enforcement": rs.enforcement,
			})
		}
		w.Write(mustJSON(list))
	case http.MethodPost:
		var payload rulesetBody
		json.Unmarshal(readBody(r), &payload)
		f.nextRulesetID++
		rs := &fakeRuleset{
			id: f.nextRulesetID, name: payload.Name, target: payload.Target,
			sourceType: "Repository", enforcement: payload.Enforcement, rules: payload.Rules,
			conditions: payload.Conditions, bypassActors: payload.BypassActors,
		}
		f.rulesets[rs.id] = rs
		w.WriteHeader(201)
		w.Write(mustJSON(rs.detail()))
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleRulesetByID(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	var id int
	fmt.Sscanf(strings.TrimPrefix(r.URL.Path, "/repos/o/r/rulesets/"), "%d", &id)
	rs, ok := f.rulesets[id]
	if !ok {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message": "Not Found"}`)
		return
	}
	switch r.Method {
	case http.MethodGet:
		w.Write(mustJSON(rs.detail()))
	case http.MethodPut:
		var payload rulesetBody
		json.Unmarshal(readBody(r), &payload)
		rs.name = payload.Name
		rs.enforcement = payload.Enforcement
		rs.rules = payload.Rules
		rs.conditions = payload.Conditions
		rs.bypassActors = payload.BypassActors
		w.Write(mustJSON(rs.detail()))
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleLabels(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	switch r.Method {
	case http.MethodGet:
		var list []map[string]any
		for _, l := range f.labels {
			list = append(list, map[string]any{
				"name": l.name, "color": l.color, "description": l.description, "default": false,
			})
		}
		w.Write(mustJSON(list))
	case http.MethodPost:
		var body struct {
			Name        string `json:"name"`
			Color       string `json:"color"`
			Description string `json:"description"`
		}
		json.Unmarshal(readBody(r), &body)
		f.labels[body.Name] = &fakeLabel{name: body.Name, color: body.Color, description: body.Description}
		w.WriteHeader(201)
		w.Write(mustJSON(map[string]any{
			"name": body.Name, "color": body.Color, "description": body.Description, "default": false,
		}))
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleLabelByName(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	name := strings.TrimPrefix(r.URL.Path, "/repos/o/r/labels/")
	l, ok := f.labels[name]
	if !ok {
		w.WriteHeader(404)
		fmt.Fprint(w, `{"message": "Not Found"}`)
		return
	}
	switch r.Method {
	case http.MethodPatch:
		var body struct {
			Color       string `json:"color"`
			Description string `json:"description"`
		}
		json.Unmarshal(readBody(r), &body)
		if body.Color != "" {
			l.color = body.Color
		}
		// Description updates always carry the canonical text;
		// an empty body field means "leave alone", never "clear".
		if body.Description != "" {
			l.description = body.Description
		}
		w.Write(mustJSON(map[string]any{
			"name": l.name, "color": l.color, "description": l.description, "default": false,
		}))
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleVulnAlerts(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	switch r.Method {
	case http.MethodGet:
		if f.vulnAlerts {
			w.WriteHeader(204)
		} else {
			w.WriteHeader(404)
		}
	case http.MethodPut:
		f.vulnAlerts = true
		w.WriteHeader(204)
	case http.MethodDelete:
		f.vulnAlerts = false
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleAutoFixes(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	switch r.Method {
	case http.MethodGet:
		if f.autoFixes {
			fmt.Fprint(w, `{"enabled": true, "paused": false}`)
		} else {
			w.WriteHeader(404)
		}
	case http.MethodPut:
		f.autoFixes = true
		fmt.Fprint(w, `{"enabled": true, "paused": false}`)
	case http.MethodDelete:
		f.autoFixes = false
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handlePrivateVulnReporting(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	switch r.Method {
	case http.MethodGet:
		fmt.Fprintf(w, `{"enabled": %t}`, f.privateVulnReporting)
	case http.MethodPut:
		f.privateVulnReporting = true
		w.WriteHeader(204)
	case http.MethodDelete:
		f.privateVulnReporting = false
		w.WriteHeader(204)
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleCodeScanning(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	switch r.Method {
	case http.MethodGet:
		if f.codeScanningSetup != "" {
			fmt.Fprintf(w, `{"state": %q, "languages": ["go"]}`, f.codeScanningSetup)
		} else {
			w.WriteHeader(404)
			fmt.Fprint(w, `{"message": "Not Found"}`)
		}
	case http.MethodPatch:
		f.codeScanningSetup = "configured"
		fmt.Fprint(w, `{"state": "configured"}`)
	default:
		w.WriteHeader(405)
	}
}

func (f *fakeGitHub) handleLanguages(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	w.Write(mustJSON(f.languages))
}

func (f *fakeGitHub) handleBranchRename(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.log(r, nil)
	if r.Method != http.MethodPost {
		w.WriteHeader(405)
		return
	}
	var body struct {
		NewName string `json:"new_name"`
	}
	json.Unmarshal(readBody(r), &body)
	f.defaultBranch = body.NewName
	w.WriteHeader(201)
	fmt.Fprint(w, `{}`)
}

// rulesetBody mirrors the create/update request body.
type rulesetBody struct {
	Name         string           `json:"name"`
	Target       string           `json:"target"`
	Enforcement  string           `json:"enforcement"`
	BypassActors []map[string]any `json:"bypass_actors"`
	Conditions   map[string]any   `json:"conditions"`
	Rules        []map[string]any `json:"rules"`
}

func (rs *fakeRuleset) detail() map[string]any {
	return map[string]any{
		"id": rs.id, "name": rs.name, "target": rs.target, "source_type": rs.sourceType,
		"enforcement": rs.enforcement, "conditions": rs.conditions,
		"bypass_actors": rs.bypassActors, "rules": rs.rules,
	}
}

func readBody(r *http.Request) []byte {
	var buf strings.Builder
	b := make([]byte, 64*1024)
	for {
		n, err := r.Body.Read(b)
		buf.Write(b[:n])
		if err != nil {
			break
		}
	}
	return []byte(buf.String())
}

func mustJSON(v any) []byte {
	out, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	if string(out) == "null" {
		return []byte("[]")
	}
	return out
}

func (f *fakeGitHub) client(t *testing.T) *ghub.Client {
	t.Helper()
	return ghub.New(ghub.WithBaseURL(f.server.URL), ghub.WithToken("test"), ghub.WithRetryBackoff(time.Millisecond))
}

func (f *fakeGitHub) mutatingRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, r := range f.requests {
		if !strings.HasPrefix(r, "GET ") {
			out = append(out, r)
		}
	}
	return out
}

// fixtures ---------------------------------------------------------------

const bootstrapPolicy = `standard:
  version: v1
repository:
  governance_profile: solo
  visibility: public
  default_branch: main
github:
  issues: true
  wiki: true
ownership:
  managed_paths:
    - LICENSE
`

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test Author", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=Test Author", "GIT_COMMITTER_EMAIL=test@example.invalid",
		"LC_ALL=C",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initRepo(t *testing.T, policy string) string {
	t.Helper()
	dir := t.TempDir()
	git(t, dir, "init", "-b", "main")
	writeFile(t, dir, "README.md", "# Fixture\n")
	writeFile(t, dir, "LICENSE", "Apache-2.0\n")
	writeFile(t, dir, "CHANGELOG.md", "# Changelog\n")
	writeFile(t, dir, "repoforge.yaml", policy)
	writeFile(t, dir, ".github/workflows/ci.yml", "name: CI\non: [push, pull_request]\njobs:\n  verify:\n    runs-on: ubuntu-latest\n    steps:\n      - run: true\n")
	git(t, dir, "add", "-A")
	git(t, dir, "commit", "-m", "chore: initial commit")
	return dir
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedOwnedRulesets inserts RepoForge-owned rulesets that exactly
// match the desired state (contexts and merge methods derived exactly
// as bootstrap derives them), for exercising the no-op and update
// paths. Call before any request reaches the fake.
func seedOwnedRulesets(t *testing.T, f *fakeGitHub, dir string) {
	t.Helper()
	_, pol, err := loadDesired(dir)
	if err != nil {
		t.Fatal(err)
	}
	wantMain, wantTags, err := configs.DesiredRulesets(pol.GovernanceProfileName(), config.DiscoverCheckContexts(dir), pol.MergeMethods())
	if err != nil {
		t.Fatal(err)
	}
	seedRuleset(t, f, 101, wantMain)
	seedRuleset(t, f, 102, wantTags)
}

func seedRuleset(t *testing.T, f *fakeGitHub, id int, p *ghub.RulesetPayload) {
	t.Helper()
	data, err := json.Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	var body rulesetBody
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}
	f.rulesets[id] = &fakeRuleset{
		id: id, name: body.Name, target: body.Target, sourceType: "Repository",
		enforcement: body.Enforcement, rules: body.Rules,
		conditions: body.Conditions, bypassActors: body.BypassActors,
	}
}

// seedCanonicalLabels inserts the canonical label set exactly as
// bootstrap wants it, for exercising the no-op and update paths.
// Call before any request reaches the fake.
func seedCanonicalLabels(t *testing.T, f *fakeGitHub) {
	t.Helper()
	want, err := configs.CanonicalLabels()
	if err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range want {
		f.labels[w.Name] = &fakeLabel{name: w.Name, color: w.Color, description: w.Description}
	}
}

// tests --------------------------------------------------------------------

func TestBootstrapConvergesAndIsIdempotent(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("first bootstrap: %v", err)
	}
	if !res.Converged {
		t.Fatalf("first bootstrap did not converge: %+v", res.Plan)
	}
	if res.Applied == 0 {
		t.Fatal("first bootstrap applied nothing on a drifted repository")
	}

	// State assertions.
	fake.mu.Lock()
	if fake.merge["allow_merge_commit"] || fake.merge["allow_rebase_merge"] || !fake.merge["delete_branch_on_merge"] {
		t.Errorf("merge settings not converged: %v", fake.merge)
	}
	if fake.features["has_projects"] || !fake.features["has_wiki"] {
		t.Errorf("features not converged: %v", fake.features)
	}
	if !fake.vulnAlerts || !fake.autoFixes {
		t.Error("dependabot not enabled")
	}
	if fake.secretScanning != "enabled" || fake.pushProtection != "enabled" {
		t.Error("secret scanning not enabled")
	}
	var haveMain, haveTags bool
	for _, rs := range fake.rulesets {
		if rs.name == configs.MainRulesetName {
			haveMain = true
		}
		if rs.name == configs.ReleaseTagsRulesetName {
			haveTags = true
		}
	}
	fake.mu.Unlock()
	if !haveMain || !haveTags {
		t.Error("owned rulesets not created")
	}

	// Idempotency: second run must plan zero changes and apply none.
	res2, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if res2.Applied != 0 {
		t.Errorf("second bootstrap applied %d changes (idempotence violated)", res2.Applied)
	}
	if !res2.Converged {
		t.Errorf("second bootstrap reports not converged: %+v", res2.Plan)
	}
	for _, item := range res2.Plan {
		if item.applicable() {
			t.Errorf("second plan still contains %s %s: %s", item.Action, item.Subject, item.Detail)
		}
	}
}

func TestBootstrapDryRunChangesNothing(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", DryRun: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	if res.Converged {
		t.Error("dry-run on drifted repo reports converged")
	}
	if muts := fake.mutatingRequests(); len(muts) > 0 {
		t.Fatalf("dry-run performed mutations: %v", muts)
	}
	if !hasApplicable(res.Plan) {
		t.Error("dry-run plan should contain applicable changes on drifted repo")
	}
}

func TestBootstrapPreservesUnmanagedRulesets(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	fake.mu.Lock()
	fake.rulesets[7] = &fakeRuleset{id: 7, name: "custom-gate", target: "branch", sourceType: "Repository", enforcement: "active", rules: []map[string]any{{"type": "creation"}}}
	fake.rulesets[8] = &fakeRuleset{id: 8, name: "org-policy", target: "branch", sourceType: "Organization", enforcement: "active", rules: []map[string]any{{"type": "deletion"}}}
	fake.mu.Unlock()

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var conflict, inherited bool
	for _, item := range res.Plan {
		if item.Action == ActionConflict && strings.Contains(item.Subject, "custom-gate") {
			conflict = true
		}
		if item.Action == ActionReport && strings.Contains(item.Subject, "org-policy") {
			inherited = true
		}
	}
	if !conflict {
		t.Error("unmanaged repository ruleset not reported as conflict")
	}
	if !inherited {
		t.Error("inherited org ruleset not reported")
	}
	for _, r := range fake.mutatingRequests() {
		if strings.Contains(r, "rulesets/7") || strings.Contains(r, "rulesets/8") {
			t.Fatalf("unmanaged ruleset was modified: %s", r)
		}
	}
	fake.mu.Lock()
	_, ok7 := fake.rulesets[7]
	_, ok8 := fake.rulesets[8]
	fake.mu.Unlock()
	if !ok7 || !ok8 {
		t.Fatal("unmanaged rulesets were deleted")
	}
}

func TestBootstrapNoopOnConformingRulesets(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	seedOwnedRulesets(t, fake, dir)

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, item := range res.Plan {
		if item.Area == "rulesets" && item.Action != ActionNoop {
			t.Errorf("ruleset plan item not no-op: %s %s: %s", item.Action, item.Subject, item.Detail)
		}
	}
	for _, r := range fake.mutatingRequests() {
		if strings.Contains(r, "/rulesets") {
			t.Errorf("conforming ruleset was mutated: %s", r)
		}
	}
}

func TestBootstrapUpdatesDriftedRulesets(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	seedOwnedRulesets(t, fake, dir)

	// Drift the seeded state: main loses enforcement and its first
	// rule; tags gains a hand-added bypass actor.
	main := fake.rulesets[101]
	main.enforcement = "disabled"
	main.rules = main.rules[1:]
	actorID := 42
	fake.rulesets[102].bypassActors = []map[string]any{
		{"actor_id": float64(actorID), "actor_type": "RepositoryRole", "bypass_mode": "always"},
	}

	// Dry-run first: the pre-apply plan must schedule both updates and
	// must not touch the API (res.Plan is the replan after apply, so
	// plan assertions have to happen before any mutation).
	resDry, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", DryRun: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	var plannedUpdates int
	for _, item := range resDry.Plan {
		if item.Area == "rulesets" && item.Action == ActionUpdate {
			plannedUpdates++
		}
	}
	if plannedUpdates != 2 {
		t.Errorf("drifted ruleset updates planned = %d, want 2: %+v", plannedUpdates, resDry.Plan)
	}
	if muts := fake.mutatingRequests(); len(muts) > 0 {
		t.Fatalf("dry-run performed mutations: %v", muts)
	}

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var putMain, putTags bool
	for _, r := range fake.mutatingRequests() {
		if r == "PUT /repos/o/r/rulesets/101" {
			putMain = true
		}
		if r == "PUT /repos/o/r/rulesets/102" {
			putTags = true
		}
	}
	if !putMain || !putTags {
		t.Errorf("expected PUTs of both owned rulesets, got main=%t tags=%t in %v", putMain, putTags, fake.mutatingRequests())
	}
	if !res.Converged {
		t.Errorf("bootstrap with ruleset updates did not converge: %+v", res.Plan)
	}

	// After apply the rulesets must read back as conforming: a second
	// run plans only no-ops and issues no ruleset mutations.
	before := len(fake.mutatingRequests())
	res2, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if res2.Applied != 0 {
		t.Errorf("second bootstrap applied %d changes (idempotence violated)", res2.Applied)
	}
	for _, item := range res2.Plan {
		if item.Area == "rulesets" && item.Action != ActionNoop {
			t.Errorf("ruleset not converged: %s %s: %s", item.Action, item.Subject, item.Detail)
		}
	}
	for _, r := range fake.mutatingRequests()[before:] {
		if strings.Contains(r, "/rulesets") {
			t.Errorf("second run mutated a ruleset: %s", r)
		}
	}
}

func TestBootstrapOwnedNameWithWrongTargetIsConflict(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	fake.rulesets[9] = &fakeRuleset{
		id: 9, name: configs.MainRulesetName, target: "tag",
		sourceType: "Repository", enforcement: "active",
	}

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var conflict bool
	for _, item := range res.Plan {
		if item.Action == ActionConflict && strings.Contains(item.Subject, configs.MainRulesetName) {
			conflict = true
		}
	}
	if !conflict {
		t.Errorf("wrong-target owned name not reported as conflict: %+v", res.Plan)
	}
	for _, r := range fake.mutatingRequests() {
		if strings.Contains(r, "rulesets/9") {
			t.Errorf("wrong-target ruleset was modified: %s", r)
		}
	}
}

func TestBootstrapCreatesMissingLabels(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	want, err := configs.CanonicalLabels()
	if err != nil {
		t.Fatal(err)
	}

	// Dry-run first: every canonical label is planned for creation
	// and nothing is mutated.
	resDry, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", DryRun: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	var creates int
	for _, item := range resDry.Plan {
		if item.Area == "labels" && item.Action == ActionCreate {
			creates++
		}
	}
	if creates != len(want) {
		t.Errorf("label creates planned = %d, want %d: %+v", creates, len(want), resDry.Plan)
	}
	if muts := fake.mutatingRequests(); len(muts) > 0 {
		t.Fatalf("dry-run performed mutations: %v", muts)
	}

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var posts int
	for _, r := range fake.mutatingRequests() {
		if r == "POST /repos/o/r/labels" {
			posts++
		}
		if strings.HasPrefix(r, "DELETE ") && strings.Contains(r, "/labels") {
			t.Errorf("bootstrap deleted a label: %s", r)
		}
	}
	if posts != len(want) {
		t.Errorf("label POSTs = %d, want %d in %v", posts, len(want), fake.mutatingRequests())
	}
	if !res.Converged {
		t.Errorf("bootstrap with label creation did not converge: %+v", res.Plan)
	}

	// Second run: labels read back as conforming — no-op and silent.
	before := len(fake.mutatingRequests())
	res2, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	if res2.Applied != 0 {
		t.Errorf("second bootstrap applied %d changes (idempotence violated)", res2.Applied)
	}
	for _, item := range res2.Plan {
		if item.Area == "labels" && item.Action != ActionNoop {
			t.Errorf("label not converged: %s %s: %s", item.Action, item.Subject, item.Detail)
		}
	}
	for _, r := range fake.mutatingRequests()[before:] {
		if strings.Contains(r, "/labels") {
			t.Errorf("second run mutated a label: %s", r)
		}
	}
}

func TestBootstrapUpdatesDriftedLabels(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	seedCanonicalLabels(t, fake)

	// Drift two labels: one color, one description.
	fake.mu.Lock()
	fake.labels["type:bug"].color = "000000"
	fake.labels["priority:p0"].description = "edited by hand"
	fake.mu.Unlock()

	// Dry-run first: both updates planned, nothing mutated.
	resDry, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", DryRun: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("dry-run: %v", err)
	}
	var updates int
	for _, item := range resDry.Plan {
		if item.Area == "labels" && item.Action == ActionUpdate {
			updates++
		}
	}
	if updates != 2 {
		t.Errorf("label updates planned = %d, want 2: %+v", updates, resDry.Plan)
	}
	if muts := fake.mutatingRequests(); len(muts) > 0 {
		t.Fatalf("dry-run performed mutations: %v", muts)
	}

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var patchBug, patchP0 bool
	for _, r := range fake.mutatingRequests() {
		if r == "PATCH /repos/o/r/labels/type:bug" {
			patchBug = true
		}
		if r == "PATCH /repos/o/r/labels/priority:p0" {
			patchP0 = true
		}
	}
	if !patchBug || !patchP0 {
		t.Errorf("expected PATCH of both drifted labels, got bug=%t p0=%t in %v", patchBug, patchP0, fake.mutatingRequests())
	}
	if !res.Converged {
		t.Errorf("bootstrap with label updates did not converge: %+v", res.Plan)
	}
}

func TestBootstrapNoopOnMatchingLabels(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	seedCanonicalLabels(t, fake)

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	for _, item := range res.Plan {
		if item.Area == "labels" && item.Action != ActionNoop {
			t.Errorf("label plan item not no-op: %s %s: %s", item.Action, item.Subject, item.Detail)
		}
	}
	for _, r := range fake.mutatingRequests() {
		if strings.Contains(r, "/labels") {
			t.Errorf("matching label was mutated: %s", r)
		}
	}
}

func TestBootstrapPreservesUnrelatedLabels(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)
	seedCanonicalLabels(t, fake)
	fake.mu.Lock()
	fake.labels["my-team"] = &fakeLabel{name: "my-team", color: "123456", description: "team-owned"}
	fake.mu.Unlock()

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var reported bool
	for _, item := range res.Plan {
		if item.Area == "labels" && item.Action == ActionReport && strings.Contains(item.Detail, "unrelated") {
			reported = true
		}
	}
	if !reported {
		t.Errorf("unrelated labels not reported as preserved: %+v", res.Plan)
	}
	for _, r := range fake.mutatingRequests() {
		if strings.Contains(r, "labels/my-team") {
			t.Fatalf("unrelated label was modified: %s", r)
		}
		if strings.HasPrefix(r, "DELETE ") && strings.Contains(r, "/labels") {
			t.Fatalf("bootstrap deleted a label: %s", r)
		}
	}
	fake.mu.Lock()
	kept, ok := fake.labels["my-team"]
	fake.mu.Unlock()
	if !ok || kept.color != "123456" || kept.description != "team-owned" {
		t.Errorf("unrelated label was not preserved: %+v", kept)
	}
}

func TestBootstrapEnablesPrivateVulnReporting(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !res.Converged {
		t.Fatalf("did not converge: %+v", res.Plan)
	}
	fake.mu.Lock()
	enabled := fake.privateVulnReporting
	fake.mu.Unlock()
	if !enabled {
		t.Error("private vulnerability reporting not enabled")
	}
	var put bool
	for _, r := range fake.mutatingRequests() {
		if r == "PUT /repos/o/r/private-vulnerability-reporting" {
			put = true
		}
	}
	if !put {
		t.Errorf("expected PUT of private-vulnerability-reporting in %v", fake.mutatingRequests())
	}
}

func TestBootstrapUnsupportedSecurityIsExplicit(t *testing.T) {
	dir := initRepo(t, strings.Replace(bootstrapPolicy, "visibility: public", "visibility: private", 1))
	fake := newFakeGitHub(t)
	fake.mu.Lock()
	fake.visibility = "private"
	fake.mu.Unlock()

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	var unsupported int
	for _, item := range res.Plan {
		if item.Action == ActionUnsupported && strings.Contains(item.Subject, "secret scanning") {
			unsupported++
		}
	}
	if unsupported == 0 {
		t.Fatal("private repo without GHAS should yield explicit unsupported items")
	}
	for _, body := range fake.patchBodies {
		if strings.Contains(body, "secret_scanning") {
			t.Fatalf("attempted doomed secret-scanning mutation on private repo: %s", body)
		}
	}
	if res.Converged {
		t.Error("run with remaining unsupported capabilities must not report converged")
	}
	if got := ExitCode(res, nil); got != 1 {
		t.Errorf("exit code = %d, want 1", got)
	}
}

func TestBootstrapConfirmationAbort(t *testing.T) {
	dir := initRepo(t, bootstrapPolicy)
	fake := newFakeGitHub(t)

	res, err := Run(context.Background(), Options{
		StartDir: dir, Repo: "o/r", Client: fake.client(t),
		Confirm: func(plan []PlanItem) bool { return false },
	})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !res.Aborted {
		t.Error("declined confirmation should mark result aborted")
	}
	if muts := fake.mutatingRequests(); len(muts) > 0 {
		t.Fatalf("aborted run mutated: %v", muts)
	}
}

func TestBootstrapCodeScanningOnPublicRepo(t *testing.T) {
	policy := strings.Replace(bootstrapPolicy, "default_branch: main", "default_branch: main\nsecurity:\n  code_scanning: true", 1)
	dir := initRepo(t, policy)
	fake := newFakeGitHub(t)

	res, err := Run(context.Background(), Options{StartDir: dir, Repo: "o/r", Yes: true, Client: fake.client(t)})
	if err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	if !res.Converged {
		t.Fatalf("did not converge: %+v", res.Plan)
	}
	fake.mu.Lock()
	setup := fake.codeScanningSetup
	fake.mu.Unlock()
	if setup != "configured" {
		t.Error("code scanning default setup not applied on public repo")
	}
	// The documented write path is PATCH (upsert); a POST has no
	// route and fails with 404 against the live API.
	var patched bool
	for _, r := range fake.mutatingRequests() {
		if r == "PATCH /repos/o/r/code-scanning/default-setup" {
			patched = true
		}
	}
	if !patched {
		t.Errorf("expected PATCH of default setup, got %v", fake.mutatingRequests())
	}
}
