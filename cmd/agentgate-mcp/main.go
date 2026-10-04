package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/Clawdlinux/agentgate/pkg/sdk"
)

// 2025-06-18 removed JSON-RPC batching, so batch arrays are intentionally unsupported.
const protocolVersion = "2025-06-18"

const maxLineBytes = 10 << 20

var errLineTooLong = errors.New("input line too long")

var envelopeKeys = map[string]bool{"jsonrpc": true, "id": true, "method": true, "params": true}

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func main() {
	log.SetFlags(0)
	key := os.Getenv("AGENTGATE_AGENT_KEY")
	if key == "" {
		log.Fatal("AGENTGATE_AGENT_KEY is required")
	}
	baseURL := os.Getenv("AGENTGATE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "https" && !(parsed.Scheme == "http" &&
			(parsed.Hostname() == "localhost" || parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "::1"))) {
		log.Fatal("AGENTGATE_URL must use HTTPS or local HTTP")
	}
	if err := serve(context.Background(), os.Stdin, os.Stdout, sdk.NewClient(strings.TrimRight(baseURL, "/"), key)); err != nil {
		log.Fatal(err)
	}
}

func serve(ctx context.Context, input io.Reader, output io.Writer, client *sdk.Client) error {
	scanner := bufio.NewScanner(input)
	scanner.Buffer(make([]byte, 4096), maxLineBytes)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		req, envelopeError := parseRequest(scanner.Bytes())
		if envelopeError != nil {
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": envelopeError}); err != nil {
				return fmt.Errorf("agentgate-mcp.serve: %w", err)
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		var result any
		var rpcError any
		switch {
		case req.Method == "initialize":
			if !validInitializeParams(req.Params) {
				rpcError = map[string]any{"code": -32602, "message": "Invalid params"}
				break
			}
			result = map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities":    map[string]any{"tools": map[string]any{}},
				"serverInfo":      map[string]string{"name": "agentgate", "version": "0.1.0-dev"},
			}
		case req.Method == "ping":
			result = map[string]any{}
		case req.Method == "tools/list":
			result = map[string]any{"tools": []any{map[string]any{
				"name":        "github_list_repos",
				"description": "List GitHub repositories for an authorized user through AgentGate",
				"inputSchema": map[string]any{
					"type": "object", "properties": map[string]any{"on_behalf_of": map[string]string{"type": "string"}},
					"required": []string{"on_behalf_of"}, "additionalProperties": false,
				},
			}}}
		case req.Method == "tools/call":
			var params struct {
				Name      string          `json:"name"`
				Arguments json.RawMessage `json:"arguments"`
			}
			if err := json.Unmarshal(req.Params, &params); err != nil || params.Name != "github_list_repos" {
				rpcError = map[string]any{"code": -32602, "message": "Invalid tool"}
				break
			}
			var args struct {
				OnBehalfOf string `json:"on_behalf_of"`
			}
			decoder := json.NewDecoder(bytes.NewReader(params.Arguments))
			decoder.DisallowUnknownFields()
			if err := decoder.Decode(&args); err != nil || strings.TrimSpace(args.OnBehalfOf) == "" {
				rpcError = map[string]any{"code": -32602, "message": "on_behalf_of is required"}
				break
			}
			response, err := client.Act(ctx, sdk.ActRequest{Service: "github", Action: "list_repos", OnBehalfOf: args.OnBehalfOf})
			if err != nil {
				result = map[string]any{"content": []any{map[string]string{"type": "text", "text": err.Error()}}, "isError": true}
				break
			}
			body, err := json.Marshal(response)
			if err != nil {
				return fmt.Errorf("agentgate-mcp.serve: %w", err)
			}
			result = map[string]any{"content": []any{map[string]string{"type": "text", "text": string(body)}}, "isError": response.Status >= 400}
		default:
			rpcError = map[string]any{"code": -32601, "message": "Method not found"}
		}
		message := map[string]any{"jsonrpc": "2.0", "id": req.ID}
		if rpcError != nil {
			message["error"] = rpcError
		} else {
			message["result"] = result
		}
		if err := encoder.Encode(message); err != nil {
			return fmt.Errorf("agentgate-mcp.serve: %w", err)
		}
	}
	if err := scanner.Err(); err != nil {
		if errors.Is(err, bufio.ErrTooLong) {
			parseError := map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "Parse error"}}
			if err := encoder.Encode(parseError); err != nil {
				return fmt.Errorf("agentgate-mcp.serve: %w", err)
			}
			return fmt.Errorf("agentgate-mcp.serve: %w", errLineTooLong)
		}
		return fmt.Errorf("agentgate-mcp.serve: %w", err)
	}
	return nil
}

// parseRequest validates the JSON-RPC 2.0 envelope. On error, req.ID holds the echoable id or nil.
func parseRequest(line []byte) (request, map[string]any) {
	if !utf8.Valid(line) || !json.Valid(line) {
		return request{}, map[string]any{"code": -32700, "message": "Parse error"}
	}
	invalid := map[string]any{"code": -32600, "message": "Invalid Request"}
	var fields map[string]json.RawMessage
	if !isJSONKind(line, '{') || hasDuplicateEnvelopeKey(line) || json.Unmarshal(line, &fields) != nil {
		return request{}, invalid
	}
	var req request
	if id, ok := fields["id"]; ok {
		if !validID(id) {
			return request{}, invalid
		}
		req.ID = id
	}
	if json.Unmarshal(fields["jsonrpc"], &req.JSONRPC) != nil || req.JSONRPC != "2.0" ||
		json.Unmarshal(fields["method"], &req.Method) != nil || req.Method == "" {
		return request{ID: req.ID}, invalid
	}
	req.Params = fields["params"]
	return req, nil
}

// hasDuplicateEnvelopeKey scans top-level keys of a valid JSON object. Nested objects are skipped.
func hasDuplicateEnvelopeKey(line []byte) bool {
	decoder := json.NewDecoder(bytes.NewReader(line))
	if _, err := decoder.Token(); err != nil {
		return true
	}
	seen := map[string]bool{}
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return true
		}
		key, _ := token.(string)
		if envelopeKeys[key] {
			if seen[key] {
				return true
			}
			seen[key] = true
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return true
		}
	}
	return false
}

func validID(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return false
	}
	switch c := trimmed[0]; {
	case c == '"', c == 'n', c == '-', c >= '0' && c <= '9':
		return true
	}
	return false
}

func validInitializeParams(raw json.RawMessage) bool {
	var params map[string]json.RawMessage
	if !isJSONKind(raw, '{') || json.Unmarshal(raw, &params) != nil {
		return false
	}
	return isJSONKind(params["protocolVersion"], '"')
}

// isJSONKind reports whether already-valid JSON starts with the given byte.
func isJSONKind(raw []byte, first byte) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && trimmed[0] == first
}
