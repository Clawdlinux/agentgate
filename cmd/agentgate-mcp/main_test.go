package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/sdk"
)

func TestServePilot(t *testing.T) {
	var calls atomic.Int32
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path != "/v1/act" || r.Header.Get("Authorization") != "Bearer scoped-key" {
			t.Errorf("unexpected gateway request: %s %s", r.URL.Path, r.Header.Get("Authorization"))
		}
		var action sdk.ActRequest
		if err := json.NewDecoder(r.Body).Decode(&action); err != nil {
			t.Error(err)
		}
		if action.Service != "github" || action.Action != "list_repos" || action.OnBehalfOf != "demo-user" {
			t.Errorf("unexpected action: %+v", action)
		}
		json.NewEncoder(w).Encode(sdk.ActResponse{Status: 200, Body: json.RawMessage(`[{"name":"example"}]`)})
	}))
	defer gateway.Close()

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"github_list_repos","arguments":{"on_behalf_of":"demo-user"}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"github_list_repos","arguments":{}}}`,
	}, "\n") + "\n"
	var output bytes.Buffer
	if err := serve(context.Background(), strings.NewReader(input), &output, sdk.NewClient(gateway.URL, "scoped-key")); err != nil {
		t.Fatal(err)
	}
	type pilotResponse struct {
		ID     int `json:"id"`
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			Tools           []struct {
				Name string `json:"name"`
			} `json:"tools"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
		Error *struct {
			Code int `json:"code"`
		} `json:"error"`
	}
	var responses []pilotResponse
	decoder := json.NewDecoder(&output)
	for decoder.More() {
		var response pilotResponse
		if err := decoder.Decode(&response); err != nil {
			t.Fatal(err)
		}
		responses = append(responses, response)
	}
	if len(responses) != 4 || responses[0].ID != 1 || responses[0].Result.ProtocolVersion != "2025-03-26" ||
		responses[1].ID != 2 || len(responses[1].Result.Tools) != 1 || responses[1].Result.Tools[0].Name != "github_list_repos" ||
		responses[2].ID != 3 || len(responses[2].Result.Content) != 1 || !strings.Contains(responses[2].Result.Content[0].Text, "example") ||
		responses[3].ID != 4 || responses[3].Error == nil || responses[3].Error.Code != -32602 || calls.Load() != 1 {
		t.Fatalf("unexpected responses: %+v; gateway calls: %d", responses, calls.Load())
	}
}

type rpcReply struct {
	ID     json.RawMessage `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Code int `json:"code"`
	} `json:"error"`
}

func serveLine(t *testing.T, line string) []rpcReply {
	t.Helper()
	var output bytes.Buffer
	if err := serve(context.Background(), strings.NewReader(line+"\n"), &output, sdk.NewClient("http://127.0.0.1:1", "scoped-key")); err != nil {
		t.Fatal(err)
	}
	var replies []rpcReply
	decoder := json.NewDecoder(&output)
	for decoder.More() {
		var reply rpcReply
		if err := decoder.Decode(&reply); err != nil {
			t.Fatalf("stdout not JSON-RPC: %v: %s", err, output.String())
		}
		replies = append(replies, reply)
	}
	return replies
}

func TestServeEnvelope(t *testing.T) {
	tests := []struct {
		name    string
		line    string
		wantOut bool
		wantID  string
		code    int
	}{
		{name: "unparseable", line: `{"jsonrpc":`, wantOut: true, wantID: "null", code: -32700},
		{name: "empty line", line: ``, wantOut: true, wantID: "null", code: -32700},
		{name: "array", line: `[1,2]`, wantOut: true, wantID: "null", code: -32600},
		{name: "string", line: `"hello"`, wantOut: true, wantID: "null", code: -32600},
		{name: "number", line: `42`, wantOut: true, wantID: "null", code: -32600},
		{name: "null literal", line: `null`, wantOut: true, wantID: "null", code: -32600},
		{name: "wrong jsonrpc", line: `{"jsonrpc":"1.0","id":1,"method":"ping"}`, wantOut: true, wantID: "1", code: -32600},
		{name: "missing jsonrpc", line: `{"id":1,"method":"ping"}`, wantOut: true, wantID: "1", code: -32600},
		{name: "missing method", line: `{"jsonrpc":"2.0","id":"a"}`, wantOut: true, wantID: `"a"`, code: -32600},
		{name: "empty method", line: `{"jsonrpc":"2.0","id":2,"method":""}`, wantOut: true, wantID: "2", code: -32600},
		{name: "non-string method", line: `{"jsonrpc":"2.0","id":2,"method":7}`, wantOut: true, wantID: "2", code: -32600},
		{name: "object id", line: `{"jsonrpc":"2.0","id":{"x":1},"method":"ping"}`, wantOut: true, wantID: "null", code: -32600},
		{name: "array id", line: `{"jsonrpc":"2.0","id":[1],"method":"ping"}`, wantOut: true, wantID: "null", code: -32600},
		{name: "bool id", line: `{"jsonrpc":"2.0","id":true,"method":"ping"}`, wantOut: true, wantID: "null", code: -32600},
		{name: "malformed notification", line: `{"jsonrpc":"1.0","method":"notifications/initialized"}`, wantOut: true, wantID: "null", code: -32600},
		{name: "notification without method", line: `{"jsonrpc":"2.0"}`, wantOut: true, wantID: "null", code: -32600},
		{name: "valid notification", line: `{"jsonrpc":"2.0","method":"notifications/initialized"}`, wantOut: false},
		{name: "string id", line: `{"jsonrpc":"2.0","id":"req-1","method":"ping"}`, wantOut: true, wantID: `"req-1"`},
		{name: "negative number id", line: `{"jsonrpc":"2.0","id":-5,"method":"ping"}`, wantOut: true, wantID: "-5"},
		{name: "null id", line: `{"jsonrpc":"2.0","id":null,"method":"ping"}`, wantOut: true, wantID: "null"},
		{name: "unknown method", line: `{"jsonrpc":"2.0","id":9,"method":"nope"}`, wantOut: true, wantID: "9", code: -32601},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replies := serveLine(t, tt.line)
			if !tt.wantOut {
				if len(replies) != 0 {
					t.Fatalf("expected no reply, got %+v", replies)
				}
				return
			}
			if len(replies) != 1 {
				t.Fatalf("expected 1 reply, got %d", len(replies))
			}
			reply := replies[0]
			if string(reply.ID) != tt.wantID {
				t.Errorf("id = %s, want %s", reply.ID, tt.wantID)
			}
			switch {
			case tt.code == 0 && reply.Error != nil:
				t.Errorf("unexpected error code %d", reply.Error.Code)
			case tt.code != 0 && (reply.Error == nil || reply.Error.Code != tt.code):
				t.Errorf("error = %+v, want code %d", reply.Error, tt.code)
			}
		})
	}
}

func TestServeInitializeParams(t *testing.T) {
	tests := []struct {
		name   string
		params string
		code   int
	}{
		{name: "valid", params: `,"params":{"protocolVersion":"2025-03-26"}`},
		{name: "valid with clientInfo", params: `,"params":{"protocolVersion":"2025-03-26","clientInfo":{"name":"c","version":"1"}}`},
		{name: "missing params", params: ``, code: -32602},
		{name: "null params", params: `,"params":null`, code: -32602},
		{name: "array params", params: `,"params":["2025-03-26"]`, code: -32602},
		{name: "missing protocolVersion", params: `,"params":{"clientInfo":{}}`, code: -32602},
		{name: "numeric protocolVersion", params: `,"params":{"protocolVersion":20250326}`, code: -32602},
		{name: "null protocolVersion", params: `,"params":{"protocolVersion":null}`, code: -32602},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			replies := serveLine(t, `{"jsonrpc":"2.0","id":1,"method":"initialize"`+tt.params+`}`)
			if len(replies) != 1 || string(replies[0].ID) != "1" {
				t.Fatalf("unexpected replies: %+v", replies)
			}
			reply := replies[0]
			if tt.code == 0 {
				if reply.Error != nil || !strings.Contains(string(reply.Result), protocolVersion) {
					t.Fatalf("expected success, got error=%+v result=%s", reply.Error, reply.Result)
				}
				return
			}
			if reply.Error == nil || reply.Error.Code != tt.code {
				t.Fatalf("error = %+v, want code %d", reply.Error, tt.code)
			}
		})
	}
}

var errBoom = errors.New("boom")

type failingIO struct{}

func (failingIO) Write([]byte) (int, error) { return 0, errBoom }
func (failingIO) Read([]byte) (int, error)  { return 0, errBoom }

func TestServeErrorWrapping(t *testing.T) {
	tests := []struct {
		name   string
		input  io.Reader
		output io.Writer
	}{
		{name: "envelope error write", input: strings.NewReader("not json\n"), output: failingIO{}},
		{name: "response write", input: strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"ping"}` + "\n"), output: failingIO{}},
		{name: "input read", input: failingIO{}, output: io.Discard},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := serve(context.Background(), tt.input, tt.output, sdk.NewClient("http://127.0.0.1:1", "scoped-key"))
			if !errors.Is(err, errBoom) || !strings.HasPrefix(err.Error(), "agentgate-mcp.serve: ") {
				t.Fatalf("err = %v, want wrapped boom", err)
			}
		})
	}
}

func TestServeGatewayDenial(t *testing.T) {
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		json.NewEncoder(w).Encode(map[string]string{"error": "key not scoped to this user", "code": "forbidden"})
	}))
	defer gateway.Close()

	input := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"github_list_repos","arguments":{"on_behalf_of":"other-user"}}}` + "\n"
	var output bytes.Buffer
	if err := serve(context.Background(), strings.NewReader(input), &output, sdk.NewClient(gateway.URL, "scoped-key")); err != nil {
		t.Fatal(err)
	}
	var response struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal(output.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if !response.Result.IsError || len(response.Result.Content) != 1 || !strings.Contains(response.Result.Content[0].Text, "key not scoped") {
		t.Fatalf("expected gateway denial as tool error, got %s", output.String())
	}
}
