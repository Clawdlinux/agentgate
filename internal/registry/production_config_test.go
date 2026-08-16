/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package registry

import "testing"

// TestProductionConfig_LoadsAndContainsFeaturedServices loads the real
// configs/services.yaml file -- the exact file the Dockerfile CMD points
// at (/etc/agentgate/configs/services.yaml) and the quickstart validates
// against -- rather than an inline test fixture. Before this test, no
// automated check ever parsed the file operators and CI actually ship;
// only synthetic fixtures were exercised elsewhere in this package.
func TestProductionConfig_LoadsAndContainsFeaturedServices(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.LoadFile("../../configs/services.yaml"); err != nil {
		t.Fatalf("load ../../configs/services.yaml: %v", err)
	}

	// CONN-03: launch documentation features exactly these three.
	for _, name := range []string{"github", "slack", "google_workspace"} {
		if _, err := r.Get(name); err != nil {
			t.Errorf("featured service %q: %v", name, err)
		}
	}

	// CONN-04: stripe remains configured and functional, just unfeatured.
	if _, err := r.Get("stripe"); err != nil {
		t.Errorf("stripe must remain functional though unfeatured: %v", err)
	}
}

// TestProductionConfig_GoogleWorkspaceRequestsNarrowScope covers CONN-02:
// the connector must request only the narrow Gmail labels scope, not a
// broader Gmail or Workspace grant.
func TestProductionConfig_GoogleWorkspaceRequestsNarrowScope(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.LoadFile("../../configs/services.yaml"); err != nil {
		t.Fatalf("load ../../configs/services.yaml: %v", err)
	}

	svc, err := r.Get("google_workspace")
	if err != nil {
		t.Fatalf("get google_workspace: %v", err)
	}
	if svc.Auth.Type != "oauth2" {
		t.Fatalf("auth.type = %s, want oauth2", svc.Auth.Type)
	}
	if len(svc.Auth.Scopes) != 1 || svc.Auth.Scopes[0] != "https://www.googleapis.com/auth/gmail.labels" {
		t.Fatalf("scopes = %v, want exactly the narrow gmail.labels scope", svc.Auth.Scopes)
	}

	_, action, err := r.GetAction("google_workspace", "list_labels")
	if err != nil {
		t.Fatalf("get list_labels action: %v", err)
	}
	if action.Method != "GET" {
		t.Fatalf("list_labels method = %s, want GET", action.Method)
	}
}

// TestProductionConfig_LocalDevDefaultMatches guards against config/
// services.yaml (the local, non-Docker dev default) silently drifting
// out of sync with configs/services.yaml (the file Docker and the
// quickstart actually load) for the featured connector set.
func TestProductionConfig_LocalDevDefaultMatches(t *testing.T) {
	t.Parallel()
	r := New()
	if err := r.LoadFile("../../config/services.yaml"); err != nil {
		t.Fatalf("load ../../config/services.yaml: %v", err)
	}
	for _, name := range []string{"github", "slack", "google_workspace", "stripe"} {
		if _, err := r.Get(name); err != nil {
			t.Errorf("config/services.yaml missing %q: %v", name, err)
		}
	}
}
