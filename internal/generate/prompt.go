package generate

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strings"
)

// TerminalPrompter asks questions on a terminal. A non-terminal stdin
// declines rather than blocking (the confirmPlan idiom, spec.md P7).
type TerminalPrompter struct {
	reader *bufio.Reader
	src    io.Reader
	out    io.Writer
}

// NewTerminalPrompter binds a prompter to a reader/writer pair.
func NewTerminalPrompter(in io.Reader, out io.Writer) *TerminalPrompter {
	return &TerminalPrompter{reader: bufio.NewReader(in), src: in, out: out}
}

// Interactive reports whether the reader is a terminal.
func (p *TerminalPrompter) Interactive() bool {
	f, ok := p.src.(*os.File)
	if !ok {
		return false
	}
	info, err := f.Stat()
	return err == nil && info.Mode()&os.ModeCharDevice != 0
}

// Ask prints the question, its guidance, and reads a line. A blank
// answer re-asks; EOF returns an error rather than inventing input.
func (p *TerminalPrompter) Ask(q Question) (string, error) {
	for {
		fmt.Fprintf(p.out, "\n%s\n", q.Question)
		if q.Help != "" {
			fmt.Fprintf(p.out, "  (%s)\n", q.Help)
		}
		fmt.Fprint(p.out, "  > ")
		line, err := p.reader.ReadString('\n')
		answer := strings.TrimSpace(line)
		if err != nil {
			if answer == "" {
				return "", fmt.Errorf("input ended while asking %q", q.Key)
			}
			return answer, nil
		}
		if answer != "" {
			return answer, nil
		}
		fmt.Fprintln(p.out, "  (an answer is required)")
	}
}

// Confirm poses a yes/no. Non-interactive input returns false.
func (p *TerminalPrompter) Confirm(prompt string) (bool, error) {
	if !p.Interactive() {
		fmt.Fprintln(p.out, "no interactive terminal; declining (nothing generated)")
		return false, nil
	}
	// Discard any buffered input (e.g., from shell autocomplete) so we
	// wait for fresh user input rather than consuming buffered content.
	if p.reader.Buffered() > 0 {
		p.reader.Discard(p.reader.Buffered())
	}
	fmt.Fprintf(p.out, "%s ", prompt)
	line, err := p.reader.ReadString('\n')
	if err != nil && strings.TrimSpace(line) == "" {
		return false, nil
	}
	answer := strings.ToLower(strings.TrimSpace(line))
	if affirmative(answer) {
		return true, nil
	}
	fmt.Fprintln(p.out, "declined; nothing generated")
	return false, nil
}

// affirmative reports whether input approves: y, ye, or yes,
// case-insensitive. Anything else — including "yes yes", empty, or
// "yeah" — declines, so pasted or hurried input fails safe.
func affirmative(answer string) bool {
	return answer == "y" || answer == "ye" || answer == "yes"
}
