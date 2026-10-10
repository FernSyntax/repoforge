package github

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestResolveExplicitToken(t *testing.T) {
	tok, err := ResolveToken(context.Background(), "explicit-token")
	if err != nil || tok != "explicit-token" {
		t.Fatalf("got %q, %v", tok, err)
	}
}

func TestResolveEnvOrder(t *testing.T) {
	t.Setenv("GH_TOKEN", "gh-token-value")
	t.Setenv("GITHUB_TOKEN", "github-token-value")

	tok, err := ResolveToken(context.Background(), "")
	if err != nil || tok != "gh-token-value" {
		t.Fatalf("GH_TOKEN should win: got %q, %v", tok, err)
	}

	t.Setenv("GH_TOKEN", "")
	tok, err = ResolveToken(context.Background(), "")
	if err != nil || tok != "github-token-value" {
		t.Fatalf("GITHUB_TOKEN should be used: got %q, %v", tok, err)
	}
}

func TestResolveGHCLI(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script fake gh")
	}
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	dir := t.TempDir()
	gh := filepath.Join(dir, "gh")
	if err := os.WriteFile(gh, []byte("#!/bin/sh\necho fake-gh-cli-token\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	tok, err := ResolveToken(context.Background(), "")
	if err != nil || tok != "fake-gh-cli-token" {
		t.Fatalf("gh fallback: got %q, %v", tok, err)
	}
}

func TestResolveNone(t *testing.T) {
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("PATH", t.TempDir()) // no gh on PATH

	_, err := ResolveToken(context.Background(), "")
	if !errors.Is(err, ErrNoCredentials) {
		t.Fatalf("expected ErrNoCredentials, got %v", err)
	}
	msg := err.Error()
	for _, source := range []string{"GH_TOKEN", "GITHUB_TOKEN", "gh auth token"} {
		if !strings.Contains(msg, source) {
			t.Errorf("error should name source %q: %v", source, msg)
		}
	}
}

func TestResolveErrorLeaksNothing(t *testing.T) {
	t.Setenv("GH_TOKEN", "secret-value-one")
	t.Setenv("GITHUB_TOKEN", "secret-value-two")
	t.Setenv("PATH", t.TempDir())
	// With env tokens set, resolution succeeds; then clear and fail.
	t.Setenv("GH_TOKEN", "")
	t.Setenv("GITHUB_TOKEN", "")
	_, err := ResolveToken(context.Background(), "")
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), "secret-value") {
		t.Fatalf("error leaks token material: %v", err)
	}
}
