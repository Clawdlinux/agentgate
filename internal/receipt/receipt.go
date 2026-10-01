/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

// Package receipt is AgentGate's gateway-side receipt plumbing: the SQLite
// ledger, the export handler, and params digesting. The protocol itself
// lives in pkg/receiptspec; the names below forward to it.
package receipt

import "github.com/Clawdlinux/agentgate/pkg/receiptspec"

// Receipt is receiptspec.Receipt.
type Receipt = receiptspec.Receipt

var (
	ErrInvalidReceipt = receiptspec.ErrInvalidReceipt
	ErrInvalidField   = receiptspec.ErrInvalidField
)

// Validate forwards to receiptspec.Validate.
var Validate = receiptspec.Validate
