// Command basic demonstrates the AgentGate Go SDK.
//
// Usage:
//
//	export AGENTGATE_URL=http://localhost:8080
//	export AGENTGATE_KEY=ag_live_...
//	go run ./examples/basic
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/Clawdlinux/agentgate/pkg/sdk"
)

func main() {
	baseURL := os.Getenv("AGENTGATE_URL")
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	apiKey := os.Getenv("AGENTGATE_KEY")
	if apiKey == "" {
		log.Fatal("AGENTGATE_KEY environment variable is required")
	}

	client := sdk.NewClient(baseURL, apiKey)
	ctx := context.Background()

	// Check health.
	if err := client.Healthz(ctx); err != nil {
		log.Fatalf("Gateway unhealthy: %v", err)
	}
	fmt.Println("Gateway is healthy!")

	// List available services.
	services, err := client.ListServices(ctx)
	if err != nil {
		log.Fatalf("List services: %v", err)
	}
	fmt.Printf("Available services: %v\n", services)

	// Call Stripe list invoices on behalf of user-42.
	resp, err := client.Stripe(ctx, "user-42", "list_invoices", map[string]interface{}{
		"limit": 5,
	})
	if err != nil {
		if sdk.IsTokenMissing(err) {
			fmt.Println("User has not linked their Stripe account yet.")
			return
		}
		log.Fatalf("Stripe call failed: %v", err)
	}

	prettyJSON, _ := json.MarshalIndent(json.RawMessage(resp.Body), "", "  ")
	fmt.Printf("Stripe response (status %d):\n%s\n", resp.Status, string(prettyJSON))
}
