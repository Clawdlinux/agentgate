/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package receipt

import "github.com/Clawdlinux/agentgate/pkg/receiptspec"

const (
	ReasonSequenceGap          = receiptspec.ReasonSequenceGap
	ReasonPrevHashMismatch     = receiptspec.ReasonPrevHashMismatch
	ReasonInvalidReceiptFields = receiptspec.ReasonInvalidReceiptFields
	ReasonEntryHashMismatch    = receiptspec.ReasonEntryHashMismatch
	ReasonSignatureInvalid     = receiptspec.ReasonSignatureInvalid
	ReasonSignerInactiveAtSeq  = receiptspec.ReasonSignerInactiveAtSeq
	ReasonExpectedHeadMismatch = receiptspec.ReasonExpectedHeadMismatch
)

var (
	ErrNoTrustedKeys    = receiptspec.ErrNoTrustedKeys
	ErrEmptyChain       = receiptspec.ErrEmptyChain
	ErrUnknownSignerKID = receiptspec.ErrUnknownSignerKID
)

type (
	TrustedKey   = receiptspec.TrustedKey
	ExpectedHead = receiptspec.ExpectedHead
	Anchor       = receiptspec.Anchor
	VerifyResult = receiptspec.VerifyResult
)

var (
	LoadTrustedKeys   = receiptspec.LoadTrustedKeys
	ParseExpectedHead = receiptspec.ParseExpectedHead
	VerifyChain       = receiptspec.VerifyChain
)
