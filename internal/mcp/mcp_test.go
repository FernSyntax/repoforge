package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// runSession feeds request lines to a server and returns its reply
// lines decoded. The server is strictly sequential, so reply order
// matches request order (notifications excepted).
func runSession(t *testing.T, root, src, product string, lines []string) []map[string]any {
	t.Helper()
	srv, err := New(root, src, product, strings.NewReader(strings.Join(lines, "\n")+"\n"), &bytes.Buffer{})
	if err != nil {
		t.Fatalf("server must construct: %v", err)
	}
	var out bytes.Buffer
	srv.out = &out
	if err := srv.Serve(context.Background()); err != nil {
		t.Fatalf("session must run clean: %v", err)
	}
	var replies []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("reply must be JSON: %v", err)
		}
		replies = append(replies, m)
	}
	return replies
}

func msg(id int, method string, params string) string {
	if params == "" {
		return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":` + quote(method) + `}`
	}
	return `{"jsonrpc":"2.0","id":` + itoa(id) + `,"method":` + quote(method) + `,"params":` + params + `}`
}

func itoa(n int) string {
	return strconv.Itoa(n)
}

func quote(s string) string {
	raw, _ := json.Marshal(s)
	return string(raw)
}

func result(t *testing.T, reply map[string]any) map[string]any {
	t.Helper()
	if reply["error"] != nil {
		t.Fatalf("expected result, got error: %v", reply["error"])
	}
	r, ok := reply["result"].(map[string]any)
	if !ok {
		t.Fatalf("result must be an object: %v", reply)
	}
	return r
}

func errCode(t *testing.T, reply map[string]any) int {
	t.Helper()
	e, ok := reply["error"].(map[string]any)
	if !ok {
		t.Fatalf("expected error, got: %v", reply)
	}
	f, ok := e["code"].(float64)
	if !ok {
		t.Fatalf("error code must be numeric: %v", e)
	}
	return int(f)
}

func callResult(t *testing.T, reply map[string]any) (string, bool) {
	t.Helper()
	r := result(t, reply)
	content, ok := r["content"].([]any)
	if !ok || len(content) != 1 {
		t.Fatalf("tool result must carry one text block: %v", r)
	}
	block, ok := content[0].(map[string]any)
	if !ok || block["type"] != "text" {
		t.Fatalf("tool result block must be text: %v", content)
	}
	text, _ := block["text"].(string)
	isErr, _ := r["isError"].(bool)
	return text, isErr
}

func call(t *testing.T, replies []map[string]any, i int) (string, bool) {
	t.Helper()
	return callResult(t, replies[i])
}

func tempRoot(t *testing.T) string {
	t.Helper()
	return t.TempDir()
}

// templateSrc locates the canonical template source from the package
// directory, skipping when the test does not run inside the checkout.
func templateSrc(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "template"))
	if err != nil {
		t.Skip("not run from the engine checkout")
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		t.Skip("not run from the engine checkout")
	}
	return src
}

// TestHandshakeAndList proves the initialize contract and the exact
// four-tool surface.
func TestHandshakeAndList(t *testing.T) {
	root := tempRoot(t)
	replies := runSession(t, root, root, "", []string{
		msg(1, "initialize", `{"protocolVersion":"2025-11-25","capabilities":{},"clientInfo":{"name":"test","version":"0"}}`),
		msg(2, "tools/list", ""),
	})
	if len(replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(replies))
	}
	init := result(t, replies[0])
	if init["protocolVersion"] != ProtocolVersion {
		t.Fatalf("server must declare %s: %v", ProtocolVersion, init)
	}
	info, _ := init["serverInfo"].(map[string]any)
	if info["name"] != "repoforge" {
		t.Fatalf("serverInfo wrong: %v", info)
	}
	list := result(t, replies[1])
	tools, _ := list["tools"].([]any)
	if len(tools) != 5 {
		t.Fatalf("exactly five tools must be listed: %v", list)
	}
	var names []string
	for _, item := range tools {
		tool, _ := item.(map[string]any)
		names = append(names, tool["name"].(string))
		if tool["inputSchema"] == nil {
			t.Fatalf("tool %v must carry an inputSchema", tool["name"])
		}
	}
	want := []string{"capabilities", "project_validate", "template_build", "template_verify", "product_build"}
	for i, n := range want {
		if names[i] != n {
			t.Fatalf("tool surface wrong: %v", names)
		}
	}
}

// TestNotificationProducesNoReply proves notifications stay silent so
// clients can interleave them freely.
func TestNotificationProducesNoReply(t *testing.T) {
	root := tempRoot(t)
	replies := runSession(t, root, root, "", []string{
		msg(1, "initialize", `{}`),
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		msg(2, "ping", ""),
	})
	if len(replies) != 2 {
		t.Fatalf("notification must not produce a reply, got %d replies", len(replies))
	}
}

// TestCapabilitiesCall proves the versions endpoint reports real
// versions and the approved tool names.
func TestCapabilitiesCall(t *testing.T) {
	root := tempRoot(t)
	replies := runSession(t, root, root, "", []string{
		msg(1, "tools/call", `{"name":"capabilities","arguments":{}}`),
	})
	text, isErr := call(t, replies, 0)
	if isErr {
		t.Fatalf("capabilities must not error: %s", text)
	}
	var caps struct {
		Engine   string   `json:"engine"`
		Protocol string   `json:"protocol"`
		Tools    []string `json:"tools"`
	}
	if err := json.Unmarshal([]byte(text), &caps); err != nil {
		t.Fatalf("capabilities must be JSON: %v", err)
	}
	if caps.Engine == "" || caps.Protocol != ProtocolVersion || len(caps.Tools) != 5 {
		t.Fatalf("capabilities wrong: %+v", caps)
	}
}

// TestProjectValidateCall proves validation round-trips through the
// tool, both directions.
func TestProjectValidateCall(t *testing.T) {
	root := tempRoot(t)
	valid := `{"name":"project_validate","arguments":{"content":"schemaVersion: v1\nname: demo\npurpose: Demo.\n","format":"yaml"}}`
	invalid := `{"name":"project_validate","arguments":{"content":"schemaVersion: v1\nname: '../x'\npurpose: Demo.\n"}}`
	replies := runSession(t, root, root, "", []string{
		msg(1, "tools/call", valid),
		msg(2, "tools/call", invalid),
	})
	text, isErr := call(t, replies, 0)
	if isErr || !strings.Contains(text, `"valid":true`) {
		t.Fatalf("valid project must validate: %s isErr=%v", text, isErr)
	}
	text, isErr = call(t, replies, 1)
	if !isErr || !strings.Contains(text, `"valid":false`) {
		t.Fatalf("invalid project must fail with violations: %s isErr=%v", text, isErr)
	}
}

// TestTemplateBuildAndVerify proves the deterministic engine path end
// to end: build, verify, rebuild refusal without force.
func TestTemplateBuildAndVerify(t *testing.T) {
	src := templateSrc(t)
	root := tempRoot(t)
	replies := runSession(t, root, src, "", []string{
		msg(1, "tools/call", `{"name":"template_build","arguments":{"outDir":"site1"}}`),
		msg(2, "tools/call", `{"name":"template_verify","arguments":{"dir":"site1"}}`),
		msg(3, "tools/call", `{"name":"template_build","arguments":{"outDir":"site1"}}`),
	})
	text, isErr := call(t, replies, 0)
	if isErr || !strings.Contains(text, `"ok":true`) {
		t.Fatalf("build must succeed: %s isErr=%v", text, isErr)
	}
	text, isErr = call(t, replies, 1)
	if isErr || !strings.Contains(text, `"ok":true`) {
		t.Fatalf("fresh build must verify: %s isErr=%v", text, isErr)
	}
	if info, err := os.Stat(filepath.Join(root, "site1")); err != nil || !info.IsDir() {
		t.Fatal("build must create the destination inside the allowed root")
	}
	text, isErr = call(t, replies, 2)
	if !isErr {
		t.Fatalf("rebuilding a non-empty destination without force must fail: %s", text)
	}
}

// TestProductBuild proves the product tool renders the Astro source
// under the same boundary, and reports cleanly when unconfigured.
func TestProductBuild(t *testing.T) {
	src := templateSrc(t)
	product := productSrc(t)
	root := tempRoot(t)
	replies := runSession(t, root, src, product, []string{
		msg(1, "tools/call", `{"name":"product_build","arguments":{"outDir":"shop","vars":{"ProjectName":"demo-site","Description":"Demo."}}}`),
		msg(2, "tools/call", `{"name":"product_build","arguments":{"outDir":"../escape","vars":{"ProjectName":"x","Description":"y"}}}`),
	})
	text, isErr := call(t, replies, 0)
	if isErr || !strings.Contains(text, `"ok":true`) {
		t.Fatalf("product build must succeed: %s isErr=%v", text, isErr)
	}
	if _, err := os.Stat(filepath.Join(root, "shop", "package.json")); err != nil {
		t.Fatalf("product build must write the site: %v", err)
	}
	if code := errCode(t, replies[1]); code != codeInvalidParams {
		t.Fatalf("product escape must be invalid params, got %d", code)
	}

	bare := runSession(t, root, src, "", []string{
		msg(1, "tools/call", `{"name":"product_build","arguments":{"outDir":"shop2"}}`),
	})
	if code := errCode(t, bare[0]); code != codeInvalidParams {
		t.Fatalf("unconfigured product_build must be invalid params, got %d", code)
	}
}

// productSrc locates the canonical Astro product source, skipping
// outside the checkout per repo pattern.
func productSrc(t *testing.T) string {
	t.Helper()
	src, err := filepath.Abs(filepath.Join("..", "..", "product", "astro"))
	if err != nil {
		t.Skip("not run from the engine checkout")
	}
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		t.Skip("not run from the engine checkout")
	}
	return src
}

// TestPathEscapesRefused proves traversal, absolute paths, and
// symlink smuggling never reach the engine.
func TestPathEscapesRefused(t *testing.T) {
	src := templateSrc(t)
	root := tempRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	replies := runSession(t, root, src, "", []string{
		msg(1, "tools/call", `{"name":"template_build","arguments":{"outDir":"../escape"}}`),
		msg(2, "tools/call", `{"name":"template_build","arguments":{"outDir":"/tmp/escape"}}`),
		msg(3, "tools/call", `{"name":"template_build","arguments":{"outDir":"link/escape"}}`),
		msg(4, "tools/call", `{"name":"template_verify","arguments":{"dir":"../escape"}}`),
	})
	for i, r := range replies {
		if code := errCode(t, r); code != codeInvalidParams {
			t.Fatalf("escape %d must be invalid params (-32602), got %d: %v", i, code, r)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "escape")); !os.IsNotExist(err) {
		t.Fatal("escape attempt must not write outside the root")
	}
}

// TestProtocolErrors proves unknown methods, tools, and malformed
// arguments get precise codes.
func TestProtocolErrors(t *testing.T) {
	root := tempRoot(t)
	replies := runSession(t, root, root, "", []string{
		msg(1, "resources/list", ""),
		msg(2, "tools/call", `{"name":"nope","arguments":{}}`),
		msg(3, "tools/call", `{"name":"project_validate","arguments":{}}`),
		msg(4, "tools/call", `{"name":"project_validate","arguments":{"content":42}}`),
	})
	if code := errCode(t, replies[0]); code != codeMethodNotFound {
		t.Fatalf("unknown method must be -32601, got %d", code)
	}
	for _, i := range []int{1, 2, 3} {
		if code := errCode(t, replies[i]); code != codeInvalidParams {
			t.Fatalf("bad tool call %d must be -32602, got %d", i, code)
		}
	}
}

// TestMalformedLineKeepsSession proves a garbage line yields a parse
// error with null id while the session continues.
func TestMalformedLineKeepsSession(t *testing.T) {
	root := tempRoot(t)
	replies := runSession(t, root, root, "", []string{
		`this is not json`,
		msg(2, "ping", ""),
	})
	if len(replies) != 2 {
		t.Fatalf("expected 2 replies, got %d", len(replies))
	}
	if code := errCode(t, replies[0]); code != codeParse {
		t.Fatalf("garbage must be -32700, got %d", code)
	}
	result(t, replies[1])
}

// TestResolveWithin proves the containment boundary directly,
// including the symlink-smuggling case.
func TestResolveWithin(t *testing.T) {
	root := tempRoot(t)
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "link")); err != nil {
		t.Skip("symlinks unavailable")
	}
	if err := os.MkdirAll(filepath.Join(root, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, err := resolveWithin(root, filepath.Join("a", "b", "c")); err != nil {
		t.Fatalf("nested new path must resolve: %v", err)
	} else if !strings.HasPrefix(got, root) {
		t.Fatalf("resolved path must stay inside root: %s", got)
	}
	for _, p := range []string{"", "/abs", "../x", "a/../../x", "link/x"} {
		if _, err := resolveWithin(root, p); err == nil {
			t.Fatalf("path %q must be refused", p)
		}
	}
}

// TestNewRejectsBadDirs proves server construction fails closed on
// missing or non-directory bindings.
func TestNewRejectsBadDirs(t *testing.T) {
	root := tempRoot(t)
	if _, err := New(filepath.Join(root, "absent"), root, "", nil, nil); err == nil {
		t.Fatal("missing allowed dir must fail")
	}
	file := filepath.Join(root, "f")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := New(file, root, "", nil, nil); err == nil {
		t.Fatal("file allowed dir must fail")
	}
	if _, err := New(root, filepath.Join(root, "absent"), "", nil, nil); err == nil {
		t.Fatal("missing template source must fail")
	}
	if _, err := New(root, root, filepath.Join(root, "absent"), nil, nil); err == nil {
		t.Fatal("missing product source must fail")
	}
}
