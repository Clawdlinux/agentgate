package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"strings"

	"github.com/Clawdlinux/agentgate/pkg/sdk"
)

const protocolVersion = "2025-03-26"

type request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

func main() {
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
	scanner.Buffer(make([]byte, 4096), 10<<20)
	encoder := json.NewEncoder(output)
	for scanner.Scan() {
		var req request
		if err := json.Unmarshal(scanner.Bytes(), &req); err != nil {
			if err := encoder.Encode(map[string]any{"jsonrpc": "2.0", "id": nil, "error": map[string]any{"code": -32700, "message": "Parse error"}}); err != nil {
				return err
			}
			continue
		}
		if len(req.ID) == 0 {
			continue
		}
		var result any
		var rpcError any
		switch {
		case req.JSONRPC != "2.0":
			rpcError = map[string]any{"code": -32600, "message": "Invalid request"}
		case req.Method == "initialize":
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
				return fmt.Errorf("encode gateway response: %w", err)
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
			return fmt.Errorf("write MCP response: %w", err)
		}
	}
	return scanner.Err()
}
