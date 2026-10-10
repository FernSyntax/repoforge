package wiki

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestGitEnvUsesBasicAuth proves the credential scheme git-over-HTTPS
// actually accepts. A bearer header is rejected with "invalid
// credentials", which would surface as an authentication failure against
// a perfectly valid token.
func TestGitEnvUsesBasicAuth(t *testing.T) {
	const token = "ghp_exampletokenvalue"
	env := gitEnv(token)

	kv := map[string]string{}
	for _, e := range env {
		if k, v, ok := strings.Cut(e, "="); ok {
			kv[k] = v
		}
	}
	if kv["GIT_CONFIG_KEY_0"] != "http.extraHeader" {
		t.Fatalf("GIT_CONFIG_KEY_0 = %q, want http.extraHeader", kv["GIT_CONFIG_KEY_0"])
	}
	header := kv["GIT_CONFIG_VALUE_0"]
	if strings.Contains(header, token) {
		t.Fatal("the raw token must not appear in the header value")
	}
	if !strings.HasPrefix(header, "Authorization: Basic ") {
		t.Fatalf("header = %q, want an Authorization: Basic header", header)
	}
	encoded := strings.TrimPrefix(header, "Authorization: Basic ")
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the header payload is not base64: %v", err)
	}
	if want := "x-access-token:" + token; string(raw) != want {
		t.Fatalf("decoded = %q, want %q", raw, want)
	}
}

// TestGitEnvWithoutTokenInjectsNoCredential proves the planning path is
// credential-free: git's own resolution (typically SSH) is left alone.
func TestGitEnvWithoutTokenInjectsNoCredential(t *testing.T) {
	kv := map[string]string{}
	for _, e := range gitEnv("") {
		if k, v, ok := strings.Cut(e, "="); ok {
			kv[k] = v
		}
	}
	if kv["GIT_CONFIG_COUNT"] != "" {
		t.Errorf("a credential-free environment must not set GIT_CONFIG_COUNT")
	}
	for _, mandatory := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_ASKPASS=true", "GIT_CONFIG_NOSYSTEM=1"} {
		found := false
		for _, e := range gitEnv("") {
			if e == mandatory {
				found = true
			}
		}
		if !found {
			t.Errorf("gitEnv is missing %s", mandatory)
		}
	}
}
