package registry

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/Clawdlinux/agentgate/internal/ratelimit"
)

func TestRegistryRateLimitSnapshot(t *testing.T) {
	r := New()
	if err := r.LoadBytes(sampleYAML); err != nil {
		t.Fatal(err)
	}
	if len(r.RateLimits()) != 0 {
		t.Fatal("existing YAML must keep service limits optional")
	}
	err := r.LoadBytes([]byte(`
services:
  github:
    base_url: https://api.github.com
    rate_limit: {requests_per_minute: 30, burst: 1}
    actions:
      list_repos: {method: GET, path: /user/repos}
`))
	if err != nil {
		t.Fatal(err)
	}
	snapshot := r.RateLimits()
	if snapshot["github"].RequestsPerMinute != 30 || snapshot["github"].Burst != 1 {
		t.Fatalf("snapshot = %+v", snapshot)
	}
	now := time.Unix(1000, 0)
	l := ratelimit.New(snapshot, ratelimit.WithSettings(ratelimit.Settings{}),
		ratelimit.WithClock(func() time.Time { return now }))
	delete(snapshot, "github")
	if len(r.RateLimits()) != 1 {
		t.Fatal("snapshot mutation changed the registry")
	}
	if l.Check("a", "alice", "github") != 0 || l.Check("a", "alice", "github") != 2*time.Second {
		t.Fatal("registry configuration was not applied to admission")
	}
	if l.Check("a", "alice", "stripe") != 0 {
		t.Fatal("unconfigured services should remain unlimited")
	}
}

func TestRegistryInvalidRateLimits(t *testing.T) {
	for _, settings := range []string{
		"requests_per_second: -1",
		"requests_per_minute: .inf",
		"requests_per_hour: .nan",
		"requests_per_second: 2, requests_per_minute: 2",
		"requests_per_second: 1, burst: -1",
		"requests_per_second: 1, burst: 1.5",
		"requests_per_hour: 5e-324",
	} {
		t.Run(settings, func(t *testing.T) {
			r := New()
			err := r.LoadBytes([]byte(fmt.Sprintf(`
services:
  github:
    base_url: https://api.github.com
    rate_limit: {%s}
`, settings)))
			if err == nil || (!strings.Contains(err.Error(), "github") && !strings.Contains(err.Error(), "parse")) {
				t.Fatalf("expected service validation error, got %v", err)
			}
			if r.Count() != 0 {
				t.Fatal("invalid service was partially loaded")
			}
		})
	}
}

func TestRegistryRateLimitYAMLAliases(t *testing.T) {
	for _, burst := range []string{"2", "1.5"} {
		t.Run(burst, func(t *testing.T) {
			r := New()
			err := r.LoadBytes([]byte(fmt.Sprintf(`
defaults: &limits
  requests_per_second: 1
  burst: &burst %s
services:
  github:
    base_url: https://api.github.com
    rate_limit: {<<: *limits}
  slack:
    base_url: https://slack.com/api
    rate_limit: {requests_per_second: 1, burst: *burst}
`, burst)))
			if (err != nil) != (burst == "1.5") {
				t.Fatalf("burst=%s err=%v", burst, err)
			}
		})
	}
}
