package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Clawdlinux/agentgate/pkg/sdk"
)

func TestServePilot(t *testing.T) {
	calls := 0
	gateway := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
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
		responses[3].ID != 4 || responses[3].Error == nil || responses[3].Error.Code != -32602 || calls != 1 {
		t.Fatalf("unexpected responses: %+v; gateway calls: %d", responses, calls)
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
