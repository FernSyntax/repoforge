package config

import (
	"fmt"
	"regexp"
	"strings"
)

// MatchPattern reports whether name matches pattern. Semantics:
// path.Match extended with "**", which matches any number of path
// segments including zero. Patterns are anchored at the repository
// root.
func MatchPattern(pattern, name string) (bool, error) {
	re, err := patternRegex(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(name), nil
}

// ValidatePattern reports whether a glob pattern is syntactically valid
// under MatchPattern semantics.
func ValidatePattern(pattern string) error {
	_, err := patternRegex(pattern)
	return err
}

func patternRegex(pattern string) (*regexp.Regexp, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty pattern")
	}
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(pattern); i++ {
		c := pattern[i]
		switch c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				i++
				if i+1 < len(pattern) && pattern[i+1] == '/' {
					i++
				}
				b.WriteString(".*")
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString("[^/]")
		case '[':
			j := strings.IndexByte(pattern[i+1:], ']')
			if j < 0 {
				return nil, fmt.Errorf("unterminated character class in %q", pattern)
			}
			b.WriteString(pattern[i : i+1+j+1])
			i += j + 1
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
		}
	}
	b.WriteString("$")
	return regexp.Compile(b.String())
}
