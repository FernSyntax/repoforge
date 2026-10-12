// Package pyruntime contains the Go question list mirroring the
// embedded Python QUESTIONS table. The parity test proves they match.
package pyruntime

// Question is one intake prompt. The Go list is the TUI's source of
// truth; the parity test proves it matches the embedded Python table
// so the two cannot drift.
type Question struct {
	Key      string
	Question string
	Help     string
}

// Questions is the world-class intake, in order. Must match
// orchestration/brief.py QUESTIONS exactly.
var Questions = []Question{
	{Key: "project_name", Question: "What is the project name?", Help: "Lowercase alphanumerics and hyphens; seeds output paths."},
	{Key: "purpose", Question: "What is the site for, in one plain sentence?", Help: "The single outcome a visitor should understand in five seconds."},
	{Key: "ideal_customer_profile", Question: "Who is your ideal customer (ICP)?", Help: "Role, company size, context — who pays and why they care."},
	{Key: "target_audience", Question: "Who else reads the site besides the ICP?", Help: "Secondary readers: evaluators, partners, hires, press."},
	{Key: "tone_of_voice", Question: "How should the copy sound?", Help: "For example: plain and direct, warm and expert, bold and terse."},
	{Key: "primary_keywords", Question: "Which search phrases must the copy serve?", Help: "Comma-separated; the phrases prospects actually type."},
	{Key: "geographic_scope", Question: "Which geographies does the site serve?", Help: "Countries, regions, or worldwide; drives locale and example choices."},
	{Key: "non_goals", Question: "What is explicitly out of scope?", Help: "What the site must not promise, claim, or become."},
	{Key: "success_metric", Question: "What does success look like?", Help: "The one conversion or behavior change the site exists to cause."},
}
