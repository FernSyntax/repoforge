package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ProtocolVersion is the MCP protocol version this server declares.
// Verified against the official Go SDK v1.8.0 sources; the subset
// spoken here (initialize, tools/list, tools/call, ping) is stable
// across the recent protocol revisions.
const ProtocolVersion = "2025-11-25"

// JSON-RPC 2.0 error codes.
const (
	codeParse          = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternal       = -32603
)

// maxMessage caps one stdio line: tool arguments carry documents, but
// a single request larger than this is a resource-exhaustion vector,
// not a project.
const maxMessage = 1 << 20

// request is one JSON-RPC 2.0 message. ID stays raw so string, number,
// and null ids all round-trip.
type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// response is one JSON-RPC 2.0 reply. Exactly one of result and err is
// set, except that error replies to notifications carry a null id.
type response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Server is a stateless MCP endpoint: every request is independent.
// Construct with New, which resolves and validates the directories.
type Server struct {
	tools *registry
	in    io.Reader
	out   io.Writer
}

// New binds the server to its directories. allowedDir must exist,
// templateSource must hold a valid template source, and productSource
// must hold a valid product source (empty disables product_build);
// all resolve to absolute symlink-free paths so later containment
// checks compare canonical locations.
func New(allowedDir, templateSource, productSource string, in io.Reader, out io.Writer) (*Server, error) {
	root, err := resolveDir(allowedDir, "allowed-dir")
	if err != nil {
		return nil, err
	}
	src, err := resolveDir(templateSource, "template-source")
	if err != nil {
		return nil, err
	}
	var product string
	if productSource != "" {
		product, err = resolveDir(productSource, "product-source")
		if err != nil {
			return nil, err
		}
	}
	return &Server{tools: newRegistry(root, src, product), in: in, out: out}, nil
}

// Serve runs the line-delimited JSON-RPC loop until stdin closes or
// the context ends. An oversized or unparseable line yields a parse
// error without killing the session; only I/O failure stops it.
func (s *Server) Serve(ctx context.Context) error {
	sc := bufio.NewScanner(s.in)
	sc.Buffer(make([]byte, 64<<10), maxMessage+1024)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if !sc.Scan() {
			if err := sc.Err(); err != nil {
				return fmt.Errorf("mcp: reading stdin: %w", err)
			}
			return nil
		}
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		if len(line) > maxMessage {
			_ = s.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeParse, Message: "message exceeds size limit"}})
			continue
		}
		var req request
		if err := json.Unmarshal(line, &req); err != nil {
			_ = s.write(response{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: codeParse, Message: "unparseable message"}})
			continue
		}
		s.dispatch(req)
	}
}

func (s *Server) write(resp response) error {
	raw, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	_, err = s.out.Write(raw)
	return err
}

// dispatch routes one message. Notifications (no id) never produce a
// reply; everything else always gets exactly one.
func (s *Server) dispatch(req request) {
	notification := len(req.ID) == 0
	reply := func(result any, rerr *rpcError) {
		if notification {
			return
		}
		id := req.ID
		if rerr != nil {
			_ = s.write(response{JSONRPC: "2.0", ID: id, Error: rerr})
			return
		}
		_ = s.write(response{JSONRPC: "2.0", ID: id, Result: result})
	}
	if req.JSONRPC != "2.0" || req.Method == "" {
		reply(nil, &rpcError{Code: codeInvalidRequest, Message: "not a JSON-RPC 2.0 method call"})
		return
	}
	defer func() {
		if r := recover(); r != nil {
			if !notification {
				_ = s.write(response{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: codeInternal, Message: fmt.Sprintf("internal error: %v", r)}})
			}
		}
	}()
	switch req.Method {
	case "initialize":
		reply(map[string]any{
			"protocolVersion": ProtocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "repoforge", "version": engineVersion()},
		}, nil)
	case "notifications/initialized":
		return
	case "ping":
		reply(map[string]any{}, nil)
	case "tools/list":
		reply(map[string]any{"tools": s.tools.describe()}, nil)
	case "tools/call":
		var params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := unmarshalParams(req.Params, &params); err != nil || params.Name == "" {
			reply(nil, &rpcError{Code: codeInvalidParams, Message: "tools/call needs {name, arguments}"})
			return
		}
		if params.Arguments == nil {
			params.Arguments = map[string]any{}
		}
		content, isErr, err := s.tools.call(params.Name, params.Arguments)
		if err != nil {
			reply(nil, &rpcError{Code: codeInvalidParams, Message: err.Error()})
			return
		}
		out := map[string]any{"content": []any{map[string]any{"type": "text", "text": content}}}
		if isErr {
			out["isError"] = true
		}
		reply(out, nil)
	default:
		reply(nil, &rpcError{Code: codeMethodNotFound, Message: "method not supported: " + req.Method})
	}
}

func unmarshalParams(raw json.RawMessage, out any) error {
	if len(raw) == 0 {
		return fmt.Errorf("missing params")
	}
	return json.Unmarshal(raw, out)
}
