/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package upstream

import (
	"errors"
	"net/http"
	"strings"
)

var errRedirectRefused = errors.New("upstream redirect refused")

func secureRedirectPolicy(previous func(*http.Request, []*http.Request) error) func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) == 0 || len(via) > 5 {
			return errRedirectRefused
		}
		original := via[0].URL
		if !strings.EqualFold(original.Scheme, req.URL.Scheme) || !strings.EqualFold(original.Host, req.URL.Host) {
			return errRedirectRefused
		}
		if previous != nil {
			return previous(req, via)
		}
		return nil
	}
}
