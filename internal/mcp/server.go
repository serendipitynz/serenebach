// Package mcp is a minimal Model Context Protocol server that exposes
// the weblog as a set of tools. The stdio transport is invoked via
// `serenebach mcp serve` — an IDE (Claude Code, Cursor, Zed, etc.)
// spawns the binary and speaks JSON-RPC 2.0 over stdin/stdout. The
// HTTP transport (mounted at /mcp by the app) gates access via Bearer
// tokens.
//
// Tool surface: read tools (list_entries, get_entry, search_entries,
// list_categories, list_tags, get_analytics, list_images) plus
// write-scope tools (create_entry, update_entry, publish_entry,
// upload_image). Read-scope tokens never see the write tools at all.
package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"

	"github.com/serendipitynz/serenebach/internal/analytics"
	"github.com/serendipitynz/serenebach/internal/domain"
	"github.com/serendipitynz/serenebach/internal/images"
	"github.com/serendipitynz/serenebach/internal/mcpaudit"
	"github.com/serendipitynz/serenebach/internal/storage/repo"
	"github.com/serendipitynz/serenebach/internal/version"
)

// ctxKey scopes the per-request auth envelope through dispatch so
// tools/list can filter the descriptor catalogue and tools/call can
// gate mutations. Values are always `*authContext`; a nil pointer is
// treated as read-only (safe default — stdio explicitly promotes to
// write before Serve wraps the context, HTTP gets the token's scope
// from the Bearer middleware).
type ctxKey struct{}

type authContext struct {
	Scope    domain.MCPScope
	TokenID  int64 // 0 for stdio / in-process callers
	AuthorID int64 // user id the token is bound to; new/updated entries attribute here
}

// WithAuth stashes the token's scope + bound author on ctx. Exposed
// so the HTTP auth wrapper can inject the Bearer-token row before
// calling HandleHTTP.
func WithAuth(ctx context.Context, scope domain.MCPScope, tokenID, authorID int64) context.Context {
	return context.WithValue(ctx, ctxKey{}, &authContext{Scope: scope, TokenID: tokenID, AuthorID: authorID})
}

// authFromContext returns the envelope, or a read-only sentinel if
// nothing was injected. Tools never branch on missing — they always
// get *some* scope.
func authFromContext(ctx context.Context) *authContext {
	if v, ok := ctx.Value(ctxKey{}).(*authContext); ok && v != nil {
		return v
	}
	return &authContext{Scope: domain.MCPScopeRead}
}

// protocolVersion is the MCP protocol revision this server speaks.
// Client-supplied versions override on the wire; this is the default
// we advertise back. 2024-11-05 is stable across current clients.
const protocolVersion = "2024-11-05"

// Server wraps the store + I/O streams so the same code can run
// against stdio (production CLI) or in-memory pipes (tests).
type Server struct {
	Store     *repo.Store
	Analytics *analytics.Store // optional; get_analytics degrades gracefully when nil
	// ImageStore is the on-disk writer for upload_image. When nil, the
	// tool returns a tool error instead of trying to persist — that way a
	// misconfigured instance (no ImageDir) fails loudly rather than
	// silently losing bytes.
	ImageStore *images.Store
	// Audit receives one row per write-tool call. Optional — when nil,
	// auditWrite still emits the machine-readable log line so write
	// activity is never invisible. Populated from main DB (via
	// WrapMain) or an external file (via mcpaudit.Open from
	// SB_MCP_AUDIT_DB).
	Audit *mcpaudit.Store
	WID   int64
	In    io.Reader
	Out   io.Writer
	// ErrLog receives transport-level error messages. Defaults to the
	// standard log package if left nil.
	ErrLog *log.Logger
}

// message is the full JSON-RPC 2.0 envelope; only the fields for the
// message's role (request vs response) are populated on any given
// read/write.
type message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

const (
	codeParseError     = -32700
	codeInvalidRequest = -32600
	codeMethodNotFound = -32601
	codeInvalidParams  = -32602
	codeInternalError  = -32603
)

// Serve reads newline-delimited JSON-RPC messages from s.In and
// writes responses to s.Out until EOF. Notifications (messages
// without an `id` field) are silently consumed. Malformed lines are
// logged and skipped rather than closing the connection — hostile
// clients shouldn't be able to wedge the server on one bad byte.
func (s *Server) Serve(ctx context.Context) error {
	if s.In == nil || s.Out == nil {
		return fmt.Errorf("mcp: In and Out must be non-nil")
	}
	// stdio transport implies local-process trust: having shell access
	// to spawn the binary is the same bar write-scoped HTTP tokens
	// clear. Wrap the root ctx once so every downstream dispatch sees
	// write scope + the seed admin (user id 1) as author — shell
	// invocations have no token row to consult for a bound author.
	ctx = WithAuth(ctx, domain.MCPScopeWrite, 0, 1)
	reader := bufio.NewReader(s.In)
	enc := json.NewEncoder(s.Out)
	for {
		// Context cancellation is the expected shutdown path for the
		// long-running stdio loop.
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		line, err := reader.ReadBytes('\n')
		if len(line) == 0 && err == io.EOF {
			return nil //nolint:nilerr // empty line + EOF is the normal end-of-input termination.
		}
		if err != nil && err != io.EOF {
			return fmt.Errorf("mcp: read: %w", err)
		}
		trimmed := trimWhitespace(line)
		if len(trimmed) == 0 {
			if err == io.EOF {
				return nil
			}
			continue
		}
		var msg message
		if jerr := json.Unmarshal(trimmed, &msg); jerr != nil {
			s.logf("mcp: parse error: %v", jerr)
			if err == io.EOF {
				return nil
			}
			continue
		}
		// Notifications carry no id and expect no reply.
		if len(msg.ID) == 0 {
			s.handleNotification(&msg)
			if err == io.EOF {
				return nil
			}
			continue
		}
		resp := s.dispatch(ctx, &msg)
		if jerr := enc.Encode(resp); jerr != nil {
			return fmt.Errorf("mcp: write: %w", jerr)
		}
		if err == io.EOF {
			return nil
		}
	}
}

func (s *Server) handleNotification(msg *message) {
	// `notifications/initialized` arrives after the client handshakes;
	// no action needed on our side, but logging at debug level helps
	// when a new client is misbehaving.
	if msg.Method != "notifications/initialized" {
		s.logf("mcp: notification: %s", msg.Method)
	}
}

// dispatch routes a request to the right method handler and returns
// the JSON-RPC response envelope. Handlers that bubble up a Go error
// get mapped to codeInternalError; parameter-validation errors bubble
// up as codeInvalidParams instead.
func (s *Server) dispatch(ctx context.Context, msg *message) *message {
	resp := &message{JSONRPC: "2.0", ID: msg.ID}
	switch msg.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities": map[string]any{
				"tools": map[string]any{},
			},
			"serverInfo": map[string]any{
				"name":    "serenebach",
				"version": version.Full(),
			},
		}
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		auth := authFromContext(ctx)
		resp.Result = map[string]any{"tools": toolDescriptors(auth.Scope)}
	case "tools/call":
		var p struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		}
		if err := json.Unmarshal(msg.Params, &p); err != nil {
			resp.Error = &rpcError{Code: codeInvalidParams, Message: "parse params: " + err.Error()}
			return resp
		}
		out, err := s.callTool(ctx, p.Name, p.Arguments)
		if err != nil {
			// Surface tool failures as MCP "tool error" payloads
			// (isError=true) per the spec — clients treat these as
			// visible-to-LLM content rather than transport errors.
			resp.Result = map[string]any{
				"isError": true,
				"content": []any{map[string]any{"type": "text", "text": err.Error()}},
			}
			return resp
		}
		resp.Result = map[string]any{
			"content": []any{map[string]any{"type": "text", "text": out}},
		}
	default:
		resp.Error = &rpcError{Code: codeMethodNotFound, Message: "unknown method: " + msg.Method}
	}
	return resp
}

func (s *Server) logf(format string, args ...any) {
	if s.ErrLog != nil {
		s.ErrLog.Printf(format, args...)
		return
	}
	log.Printf(format, args...)
}

// HandleHTTP implements the Streamable HTTP transport from the
// 2025-03-26 MCP spec: clients POST a single JSON-RPC message and the
// server replies with an application/json response carrying exactly
// one result or error envelope. Our tools are synchronous and don't
// produce server-initiated messages, so we skip the text/event-stream
// branch entirely — clients that only declare `Accept: application/
// json` get a plain JSON response, and clients that declare both
// still work because application/json is a valid spec choice.
//
// Caller is responsible for authorising the request *before* dispatch;
// this method treats every incoming request as trusted.
func (s *Server) HandleHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// GET on /mcp is reserved for the optional server→client push
		// stream. We don't emit notifications, so return 405 so
		// clients fall through to POST-only mode.
		w.Header().Set("Allow", http.MethodPost)
		http.Error(w, "only POST is supported", http.StatusMethodNotAllowed)
		return
	}
	defer r.Body.Close()
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20)) // 1 MiB cap
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	body = trimWhitespace(body)
	if len(body) == 0 {
		http.Error(w, "empty body", http.StatusBadRequest)
		return
	}

	var msg message
	if jerr := json.Unmarshal(body, &msg); jerr != nil {
		// Parse-error response must carry a null id per JSON-RPC 2.0.
		writeJSONRPC(w, &message{
			JSONRPC: "2.0",
			Error:   &rpcError{Code: codeParseError, Message: jerr.Error()},
		})
		return
	}
	// Notifications expect no response body; spec says 202 Accepted.
	if len(msg.ID) == 0 {
		s.handleNotification(&msg)
		w.WriteHeader(http.StatusAccepted)
		return
	}

	resp := s.dispatch(r.Context(), &msg)
	writeJSONRPC(w, resp)
}

func writeJSONRPC(w http.ResponseWriter, resp *message) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(resp)
}

func trimWhitespace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}
