package bootstrap

import (
	"context"
	"sort"
	"strings"

	"github.com/FernSyntax/repoforge/internal/config"
	ghub "github.com/FernSyntax/repoforge/internal/github"
)

// actualState is the remote repository state read once per cycle
// (read-only GETs).
type actualState struct {
	repo       *ghub.Repository
	topics     []string
	rulesets   []ghub.Ruleset
	mainDetail *ghub.RulesetDetail
	tagsDetail *ghub.RulesetDetail
	labels     []ghub.Label
	security   *ghub.SecuritySettings
	languages  map[string]int
}

func readActual(ctx context.Context, client *ghub.Client, owner, repoName string) (*actualState, error) {
	s := &actualState{}
	var err error

	if s.repo, err = client.GetRepository(ctx, owner, repoName); err != nil {
		return nil, err
	}
	t, err := client.ListTopics(ctx, owner, repoName)
	if err != nil {
		return nil, err
	}
	s.topics = t.Names

	if s.rulesets, err = client.ListRulesets(ctx, owner, repoName); err != nil {
		return nil, err
	}
	for _, rs := range s.rulesets {
		if rs.Target == "branch" && rs.Name == mainRulesetName && s.mainDetail == nil {
			d, err := client.GetRuleset(ctx, owner, repoName, rs.ID)
			if err != nil {
				return nil, err
			}
			s.mainDetail = d
		}
		if rs.Target == "tag" && rs.Name == releaseTagsRulesetName && s.tagsDetail == nil {
			d, err := client.GetRuleset(ctx, owner, repoName, rs.ID)
			if err != nil {
				return nil, err
			}
			s.tagsDetail = d
		}
	}

	if s.security, err = client.GetSecuritySettings(ctx, owner, repoName); err != nil {
		return nil, err
	}
	if s.labels, err = client.ListLabels(ctx, owner, repoName); err != nil {
		return nil, err
	}
	if s.languages, err = client.ListLanguages(ctx, owner, repoName); err != nil {
		return nil, err
	}
	return s, nil
}

func mergePatch(pol *config.Policy, r *ghub.Repository) (ghub.RepoSettingsPatch, []string) {
	var patch ghub.RepoSettingsPatch
	var changes []string
	set := func(name string, actual, want bool, assign func(*bool)) {
		if actual != want {
			b := want
			assign(&b)
			changes = append(changes, name)
		}
	}
	set("has_issues", r.HasIssues, pol.GitHubIssues(), func(b *bool) { patch.HasIssues = b })
	set("has_discussions", r.HasDiscussions, pol.GitHub.Discussions, func(b *bool) { patch.HasDiscussions = b })
	set("has_projects", r.HasProjects, pol.GitHub.Projects, func(b *bool) { patch.HasProjects = b })
	set("has_wiki", r.HasWiki, pol.GitHub.Wiki, func(b *bool) { patch.HasWiki = b })
	set("allow_squash_merge", r.AllowSquashMerge, pol.MergeSquash(), func(b *bool) { patch.AllowSquashMerge = b })
	set("allow_merge_commit", r.AllowMergeCommit, pol.Merge.MergeCommit, func(b *bool) { patch.AllowMergeCommit = b })
	set("allow_rebase_merge", r.AllowRebaseMerge, pol.Merge.Rebase, func(b *bool) { patch.AllowRebaseMerge = b })
	set("delete_branch_on_merge", r.DeleteBranchOnMerge, pol.MergeDeleteBranch(), func(b *bool) { patch.DeleteBranchOnMerge = b })
	set("allow_auto_merge", r.AllowAutoMerge, pol.Merge.AutoMerge, func(b *bool) { patch.AllowAutoMerge = b })
	return patch, changes
}

func sameStringSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	as := append([]string(nil), a...)
	bs := append([]string(nil), b...)
	sort.Strings(as)
	sort.Strings(bs)
	return strings.Join(as, ",") == strings.Join(bs, ",")
}
