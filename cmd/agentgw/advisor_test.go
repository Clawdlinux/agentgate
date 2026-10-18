package main

import "testing"

func TestAdvisorConfig(t *testing.T) {
	t.Setenv("AGENTGATE_ADVISOR_URL", "")
	client, p, err := loadAdvisor()
	if err != nil || client != nil || p.Unavailable != "allow_with_flag" {
		t.Fatal("not disabled by default")
	}
	t.Setenv("AGENTGATE_ADVISOR_URL", "http://localhost:8081")
	client, _, err = loadAdvisor()
	if err != nil || client == nil {
		t.Fatalf("enabled: %v", err)
	}
	client.CloseIdleConnections()
	for _, tc := range []struct{ name, value string }{
		{"AGENTGATE_ADVISOR_APPROVAL_THRESHOLD", "NaN"},
		{"AGENTGATE_ADVISOR_DENY_THRESHOLD", "0.3"},
		{"AGENTGATE_ADVISOR_CONFIDENCE_THRESHOLD", "1.1"},
		{"AGENTGATE_ADVISOR_UNAVAILABLE", "ignore"},
		{"AGENTGATE_ADVISOR_URL", "https://example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(tc.name, tc.value)
			if _, _, err := loadAdvisor(); err == nil {
				t.Fatal("accepted invalid config")
			}
		})
	}
}
