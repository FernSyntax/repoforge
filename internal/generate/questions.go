package generate

// Question is one intake prompt. The Go list is the TUI's source of
// truth; the parity test proves it matches the embedded Python table
// so the two cannot drift.
type Question struct {
	Key      string
	Question string
	Help     string
}

// Questions is the world-class intake, in order.
var Questions = []Question{
	{"project_name", "What is the project name?",
		"Lowercase alphanumerics and hyphens; seeds output paths."},
	{"purpose", "What is the site for, in one plain sentence?",
		"The single outcome a visitor should understand in five seconds."},
	{"ideal_customer_profile", "Who is your ideal customer (ICP)?",
		"Role, company size, context — who pays and why they care."},
	{"target_audience", "Who else reads the site besides the ICP?",
		"Secondary readers: evaluators, partners, hires, press."},
	{"tone_of_voice", "How should the copy sound?",
		"For example: plain and direct, warm and expert, bold and terse."},
	{"primary_keywords", "Which search phrases must the copy serve?",
		"Comma-separated; the phrases prospects actually type."},
	{"geographic_scope", "Which geographies does the site serve?",
		"Countries, regions, or worldwide; drives locale and example choices."},
	{"non_goals", "What is explicitly out of scope?",
		"What the site must not promise, claim, or become."},
	{"success_metric", "What does success look like?",
		"The one conversion or behavior change the site exists to cause."},
}
