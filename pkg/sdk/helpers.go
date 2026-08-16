package sdk

import "context"

// Stripe calls a Stripe action via the gateway.
func (c *Client) Stripe(ctx context.Context, userID, action string, params map[string]interface{}) (*ActResponse, error) {
	return c.Act(ctx, ActRequest{
		Service:    "stripe",
		Action:     action,
		OnBehalfOf: userID,
		Params:     params,
	})
}

// GitHub calls a GitHub action via the gateway.
func (c *Client) GitHub(ctx context.Context, userID, action string, params map[string]interface{}) (*ActResponse, error) {
	return c.Act(ctx, ActRequest{
		Service:    "github",
		Action:     action,
		OnBehalfOf: userID,
		Params:     params,
	})
}

// Slack calls a Slack action via the gateway.
func (c *Client) Slack(ctx context.Context, userID, action string, params map[string]interface{}) (*ActResponse, error) {
	return c.Act(ctx, ActRequest{
		Service:    "slack",
		Action:     action,
		OnBehalfOf: userID,
		Params:     params,
	})
}

// GoogleWorkspace calls a Google Workspace action via the gateway.
func (c *Client) GoogleWorkspace(ctx context.Context, userID, action string, params map[string]interface{}) (*ActResponse, error) {
	return c.Act(ctx, ActRequest{
		Service:    "google_workspace",
		Action:     action,
		OnBehalfOf: userID,
		Params:     params,
	})
}
