package main

import (
	"fmt"
	"os"
	"strconv"

	"github.com/Clawdlinux/agentgate/internal/advisor"
)

func loadAdvisor() (*advisor.HTTPAdvisor, advisor.Policy, error) {
	p := advisor.DefaultPolicy()
	rawURL := os.Getenv("AGENTGATE_ADVISOR_URL")
	if rawURL == "" {
		return nil, p, nil
	}
	for _, entry := range []struct {
		name   string
		target *float64
	}{
		{"AGENTGATE_ADVISOR_APPROVAL_THRESHOLD", &p.ApprovalThreshold},
		{"AGENTGATE_ADVISOR_DENY_THRESHOLD", &p.DenyThreshold},
		{"AGENTGATE_ADVISOR_CONFIDENCE_THRESHOLD", &p.ConfidenceThreshold},
	} {
		if value, ok := os.LookupEnv(entry.name); ok {
			f, err := strconv.ParseFloat(value, 64)
			if err != nil {
				return nil, p, fmt.Errorf("%s: expected probability", entry.name)
			}
			*entry.target = f
		}
	}
	if value, ok := os.LookupEnv("AGENTGATE_ADVISOR_UNAVAILABLE"); ok {
		p.Unavailable = value
	}
	if err := p.Validate(); err != nil {
		return nil, p, err
	}
	client, err := advisor.NewHTTPAdvisor(rawURL)
	return client, p, err
}
