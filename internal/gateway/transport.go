/*
Copyright 2026 Clawdlinux.
Licensed under the Apache License, Version 2.0.
*/

package gateway

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
)

// CloseIdleConnections releases upstream connections after HTTP shutdown.
func (s *Server) CloseIdleConnections() {
	s.upstreams.CloseIdleConnections()
}

func (s *Server) logUpstreamError(service string, err error) {
	// Error strings can contain URLs, query credentials, or custom transport secrets.
	// Log structured diagnostics instead. Never log the request or its headers.
	cause := err
	for {
		urlErr, ok := cause.(*url.Error)
		if !ok {
			break
		}
		cause = urlErr.Err
	}
	fields := []any{
		"service", service,
		"error_type", fmt.Sprintf("%T", cause),
		"canceled", errors.Is(err, context.Canceled),
		"deadline_exceeded", errors.Is(err, context.DeadlineExceeded),
		"connection_refused", errors.Is(err, syscall.ECONNREFUSED),
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		fields = append(fields, "timeout", netErr.Timeout())
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		fields = append(fields, "operation", opErr.Op, "network", opErr.Net, "cause_type", fmt.Sprintf("%T", opErr.Err))
	}
	s.logger.Error("upstream request failed", fields...)
}
