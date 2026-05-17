package registry

import (
	"errors"
	"reflect"
	"testing"
)

var sampleYAML = []byte(`
services:
  github:
    base_url: https://api.github.com
    auth:
      type: oauth2
      authorize_url: https://github.com/login/oauth/authorize
      token_url: https://github.com/login/oauth/access_token
      scopes: [repo, read:org]
    actions:
      list_repos:
        method: GET
        path: /user/repos
        params:
          type: "string?"
          sort: "string?"
      create_issue:
        method: POST
        path: /repos/{owner}/{repo}/issues
        params:
          owner: string
          repo: string
          title: string
          body: "string?"
  stripe:
    base_url: https://api.stripe.com/v1
    auth:
      type: bearer
    actions:
      list_invoices:
        method: GET
        path: /invoices
        params:
          customer: "string?"
          limit: "int?"
`)

func TestRegistry_LoadBytes(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.LoadBytes(sampleYAML); err != nil {
		t.Fatalf("load: %v", err)
	}
	if r.Count() != 2 {
		t.Fatalf("count = %d, want 2", r.Count())
	}
}

func TestRegistry_Get(t *testing.T) {
	t.Parallel()
	r := New()
	_ = r.LoadBytes(sampleYAML)

	svc, err := r.Get("github")
	if err != nil {
		t.Fatalf("get github: %v", err)
	}
	if svc.BaseURL != "https://api.github.com" {
		t.Fatalf("base_url = %s", svc.BaseURL)
	}
	if svc.Auth.Type != "oauth2" {
		t.Fatalf("auth.type = %s", svc.Auth.Type)
	}
	if !reflect.DeepEqual(svc.Auth.Scopes, []string{"repo", "read:org"}) {
		t.Fatalf("scopes = %v", svc.Auth.Scopes)
	}
}

func TestRegistry_GetAction(t *testing.T) {
	t.Parallel()
	r := New()
	_ = r.LoadBytes(sampleYAML)

	svc, action, err := r.GetAction("github", "create_issue")
	if err != nil {
		t.Fatalf("get action: %v", err)
	}
	if svc.Name != "github" {
		t.Fatalf("svc name = %s", svc.Name)
	}
	if action.Method != "POST" {
		t.Fatalf("method = %s", action.Method)
	}
	if action.Path != "/repos/{owner}/{repo}/issues" {
		t.Fatalf("path = %s", action.Path)
	}
	if action.Params["title"] != "string" {
		t.Fatalf("params.title = %s", action.Params["title"])
	}
}

func TestRegistry_NotFound(t *testing.T) {
	t.Parallel()
	r := New()
	_ = r.LoadBytes(sampleYAML)

	_, err := r.Get("nonexistent")
	if !errors.Is(err, ErrServiceNotFound) {
		t.Fatalf("expected ErrServiceNotFound, got %v", err)
	}

	_, _, err = r.GetAction("github", "nonexistent")
	if !errors.Is(err, ErrActionNotFound) {
		t.Fatalf("expected ErrActionNotFound, got %v", err)
	}
}

func TestRegistry_List(t *testing.T) {
	t.Parallel()
	r := New()
	_ = r.LoadBytes(sampleYAML)

	names := r.List()
	if !reflect.DeepEqual(names, []string{"github", "stripe"}) {
		t.Fatalf("list = %v", names)
	}
}
