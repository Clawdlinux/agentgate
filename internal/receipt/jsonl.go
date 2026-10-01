/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import "github.com/Clawdlinux/agentgate/pkg/receiptspec"

// JSONLFormatVersion is receiptspec.JSONLFormatVersion.
const JSONLFormatVersion = receiptspec.JSONLFormatVersion

var (
	ErrUnsupportedFormatVersion = receiptspec.ErrUnsupportedFormatVersion
	MarshalJSONLReceipt         = receiptspec.MarshalJSONLReceipt
	ParseJSONLReceipt           = receiptspec.ParseJSONLReceipt
)
