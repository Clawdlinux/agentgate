package registry

import (
	"fmt"
	"testing"
)

func TestAdvisorServiceIsReserved(t *testing.T) {
	for _, name := range []string{"advisor", "Advisor", "ADVISOR"} {
		r := New()
		data := fmt.Sprintf("services:\n  %s:\n    base_url: https://example.com\n    actions: {}\n", name)
		if err := r.LoadBytes([]byte(data)); err == nil {
			t.Fatalf("accepted reserved service %s", name)
		}
		if r.Count() != 0 {
			t.Fatal("partially loaded reserved service")
		}
	}
}
