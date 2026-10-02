/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import "github.com/Clawdlinux/agentgate/pkg/receiptspec"

const (
	ExportManifestDomainV1 = receiptspec.ExportManifestDomainV1
	ExportFormatVersion    = receiptspec.ExportFormatVersion
)

var (
	ErrManifestSignatureInvalid = receiptspec.ErrManifestSignatureInvalid
	ErrKeysetDigestMismatch     = receiptspec.ErrKeysetDigestMismatch
)

// ExportManifest is receiptspec.ExportManifest.
type ExportManifest = receiptspec.ExportManifest

var (
	ComputeKeysetDigest = receiptspec.ComputeKeysetDigest
	ComputeManifestHash = receiptspec.ComputeManifestHash
	SignManifest        = receiptspec.SignManifest
	VerifyManifest      = receiptspec.VerifyManifest
	DetectJSONLLineType = receiptspec.DetectJSONLLineType
	MarshalManifestLine = receiptspec.MarshalManifestLine
	ParseManifestLine   = receiptspec.ParseManifestLine
	MarshalKeyLine      = receiptspec.MarshalKeyLine
	ParseKeyLine        = receiptspec.ParseKeyLine
)
