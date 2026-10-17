/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package delegation

import (
	"errors"
	"fmt"

	biscuit "github.com/biscuit-auth/biscuit-go/v2"
	"github.com/biscuit-auth/biscuit-go/v2/datalog"
	"github.com/biscuit-auth/biscuit-go/v2/pb"
	"google.golang.org/protobuf/proto"
)

// ErrTokenShape means token structure exceeds the supported delegation profile.
var ErrTokenShape = errors.New("delegation: unsupported token shape")

const (
	maxChecksPerBlock      = 16
	maxPredicatesPerQuery  = 4
	maxExpressionsPerQuery = 8
	maxOpsPerExpression    = 32
)

func denyTokenShape(reason string) error {
	return fmt.Errorf("%w: %w: %s", ErrDenied, ErrTokenShape, reason)
}

func (v *Verifier) checkTokenShape(token []byte, b *biscuit.Biscuit) error {
	if b.BlockCount() > v.limits.MaxBlocks {
		return denyTokenShape("too many attenuation blocks")
	}

	// Code omits authority and wraps blocks in debug text. Inspect the public wire types instead.
	var container pb.Biscuit
	if err := proto.Unmarshal(token, &container); err != nil {
		return denyTokenShape("invalid token encoding")
	}
	blocks := append([]*pb.SignedBlock{container.GetAuthority()}, container.GetBlocks()...)
	for i, signed := range blocks {
		var block pb.Block
		if err := proto.Unmarshal(signed.GetBlock(), &block); err != nil {
			return denyTokenShape("invalid block encoding")
		}
		if len(block.GetRulesV2()) != 0 {
			return denyTokenShape("rules are not allowed")
		}
		if i == 0 {
			if !issuedGrantShape(&block) {
				return denyTokenShape("authority must contain exactly one issued grant fact")
			}
		} else if len(block.GetFactsV2()) != 0 {
			return denyTokenShape("attenuation facts are not allowed")
		}
		if len(block.GetChecksV2()) > maxChecksPerBlock {
			return denyTokenShape("too many checks in block")
		}
		for _, check := range block.GetChecksV2() {
			for _, query := range check.GetQueries() {
				if len(query.GetBody()) > maxPredicatesPerQuery {
					return denyTokenShape("too many predicates in check query")
				}
				if len(query.GetExpressions()) > maxExpressionsPerQuery {
					return denyTokenShape("too many expressions in check query")
				}
				for _, expression := range query.GetExpressions() {
					if len(expression.GetOps()) > maxOpsPerExpression {
						return denyTokenShape("too many operations in check expression")
					}
					for _, op := range expression.GetOps() {
						if binary := op.GetBinary(); binary != nil && binary.GetKind() == pb.OpBinary_Regex {
							return denyTokenShape("regex expressions are not allowed")
						}
					}
				}
			}
		}
	}
	return nil
}

func issuedGrantShape(block *pb.Block) bool {
	if len(block.GetFactsV2()) != 1 {
		return false
	}
	predicate := block.GetFactsV2()[0].GetPredicate()
	symbols := datalog.SymbolTable(block.GetSymbols())
	grantSymbol, ok := symbols.Sym(grantFactName).(datalog.String)
	if !ok || predicate.GetName() != uint64(grantSymbol) || len(predicate.GetTerms()) != 5 {
		return false
	}
	terms := predicate.GetTerms()
	for _, term := range terms[:4] {
		value, ok := term.GetContent().(*pb.TermV2_String_)
		if !ok {
			return false
		}
		id := value.String_
		if id < uint64(datalog.OFFSET) {
			if id >= uint64(len(datalog.DEFAULT_SYMBOLS)) {
				return false
			}
		} else if id-uint64(datalog.OFFSET) >= uint64(len(symbols)) {
			return false
		}
	}
	_, ok = terms[4].GetContent().(*pb.TermV2_Date)
	return ok
}
