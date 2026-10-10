package github

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// Path errors. A path component that cannot be encoded safely is refused
// before any request is built: an owner or repository name is data, not
// a path segment, and interpolating it directly lets a name containing
// "/", "..", "?" or "#" steer the request elsewhere.
var (
	// ErrUnsafePathComponent reports a component that would change the
	// shape or target of a request path.
	ErrUnsafePathComponent = fmt.Errorf("github: unsafe path component")
)

// isSafeComponent reports whether s is a single, encodable path segment.
//
// Refused: empty strings; "." and ".."; anything containing "/", "?",
// "#", or "%"; NUL and other control characters; and whitespace, which
// the API never accepts and which would be sent raw in a request line.
func isSafeComponent(s string) bool {
	if s == "" || s == "." || s == ".." {
		return false
	}
	for _, r := range s {
		switch {
		case r == '/' || r == '?' || r == '#' || r == '%':
			return false
		case r < 0x20 || r == 0x7f:
			return false
		case r == ' ' || r == '\t':
			return false
		}
	}
	return true
}

// pathSegment validates and encodes one path component.
func pathSegment(name, value string) (string, error) {
	if !isSafeComponent(value) {
		return "", fmt.Errorf("%w: %s %q is not a single safe path segment", ErrUnsafePathComponent, name, value)
	}
	return url.PathEscape(value), nil
}

// repoPath builds an API path under /repos/{owner}/{repo} and appends
// any further validated, escaped segments. Every component is checked
// before the path is assembled, so a malformed slug fails here with an
// actionable error instead of producing a request to an unintended
// endpoint.
func repoPath(owner, repo string, elems ...string) (string, error) {
	o, err := pathSegment("owner", owner)
	if err != nil {
		return "", err
	}
	r, err := pathSegment("repository", repo)
	if err != nil {
		return "", err
	}
	parts := []string{o, r}
	for _, e := range elems {
		v, err := pathSegment("path component", e)
		if err != nil {
			return "", err
		}
		parts = append(parts, v)
	}
	return "/repos/" + strings.Join(parts, "/"), nil
}

// repoPathOrPanic is the internal shorthand for the (many) call sites
// whose components are already validated: enums, fixed segments, and
// numeric IDs. It panics only on a programming error, never on data.
func repoPathOrPanic(owner, repo string, elems ...string) string {
	for _, e := range append([]string{owner, repo}, elems...) {
		if !isSafeComponent(e) {
			panic("github: unsafe hardcoded path component: " + strconv.Quote(e))
		}
	}
	parts := make([]string, 0, len(elems)+3)
	parts = append(parts, "repos", owner, repo)
	for _, e := range elems {
		parts = append(parts, e)
	}
	return "/" + strings.Join(parts, "/")
}

// refPath builds a path for a Git ref, whose names legitimately contain
// "/" (feature/topic/sub). Slashes are structure, not data, so each
// segment is validated and escaped individually and the hierarchy is
// preserved. An empty segment, "." or ".." would escape the ref
// hierarchy, so those are refused.
func refPath(base string, name string) (string, error) {
	if name == "" {
		return "", fmt.Errorf("%w: ref name is empty", ErrUnsafePathComponent)
	}
	if strings.ContainsAny(name, "?#= \t\n") || strings.ContainsRune(name, 0) {
		return "", fmt.Errorf("%w: ref name %q contains a character that is not valid in a ref", ErrUnsafePathComponent, name)
	}
	segments := strings.Split(name, "/")
	for i, seg := range segments {
		switch seg {
		case "", ".", "..":
			return "", fmt.Errorf("%w: ref name %q has an invalid segment %q at position %d",
				ErrUnsafePathComponent, name, seg, i)
		}
		segments[i] = url.PathEscape(seg)
	}
	return base + "/" + strings.Join(segments, "/"), nil
}

// branchPath validates and encodes both branch names in a rename
// request (POST /repos/{owner}/{repo}/branches/{branch}/rename).
func branchPath(owner, repo, branch, newName string) (string, error) {
	base, err := repoPath(owner, repo, "branches")
	if err != nil {
		return "", err
	}
	ref, err := refPath(base, branch)
	if err != nil {
		return "", fmt.Errorf("branch name %q is not a single safe ref: %w", branch, err)
	}
	if err := validateRefComponent("new branch name", newName); err != nil {
		return "", err
	}
	return ref + "/rename", nil
}

// validateRefComponent checks a ref name that appears only in a request
// body, where "/" is ordinary data rather than separator structure.
func validateRefComponent(what, name string) error {
	if name == "" {
		return fmt.Errorf("%w: %s is empty", ErrUnsafePathComponent, what)
	}
	if strings.ContainsAny(name, "\t\n") || strings.ContainsRune(name, 0) {
		return fmt.Errorf("%w: %s %q contains a control character", ErrUnsafePathComponent, what, name)
	}
	return nil
}

// labelPath validates and encodes a label name
// (PATCH /repos/{owner}/{repo}/labels/{name}). Label names are data and
// may legitimately contain "/" in other tools, so an unencodable one is
// refused rather than silently truncated.
func labelPath(owner, repo, name string) (string, error) {
	l, err := pathSegment("label name", name)
	if err != nil {
		return "", err
	}
	base, err := repoPath(owner, repo, "labels")
	if err != nil {
		return "", err
	}
	return base + "/" + l, nil
}
