package gateway

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type streamResponse struct {
	body      io.ReadCloser
	limit     int64
	idle      time.Duration
	closeOnce sync.Once
}

func (s *streamResponse) close() {
	s.closeOnce.Do(func() { s.body.Close() })
}

func acceptsEventStream(header http.Header) bool {
	for _, value := range header.Values("Accept") {
		for _, part := range strings.Split(value, ",") {
			mediaType, params, err := mime.ParseMediaType(strings.TrimSpace(part))
			if err != nil || mediaType != "text/event-stream" {
				continue
			}
			if q, ok := params["q"]; ok {
				quality, err := strconv.ParseFloat(q, 64)
				if err != nil || !(quality > 0 && quality <= 1) {
					continue
				}
			}
			return true
		}
	}
	return false
}

// writeStream runs only after the header-time receipt commits.
func (s *Server) writeStream(w http.ResponseWriter, r *http.Request, att *attempt, stream *streamResponse) {
	digest := sha256.New()
	var count int64
	var reason atomic.Pointer[string]
	stop := func(value string) { reason.CompareAndSwap(nil, &value) }
	idleDeadline := time.Now().Add(stream.idle)
	writeFailed := func() {
		switch {
		case r.Context().Err() != nil:
			stop("client_disconnect")
		case !time.Now().Before(idleDeadline):
			stop("idle_timeout")
		default:
			stop("client_write_error")
		}
	}
	timer := time.AfterFunc(stream.idle, func() {
		stop("idle_timeout")
		stream.close()
	})
	controller := http.NewResponseController(w)
	defer func() {
		timer.Stop()
		stream.close()
		controller.SetWriteDeadline(time.Time{})
		stop("completed")
		s.logger.Info("stream finished",
			"agent", att.agentKey.ID,
			"service", att.req.Service,
			"action", att.req.Action,
			"sha256", hex.EncodeToString(digest.Sum(nil)),
			"bytes", count,
			"reason", *reason.Load(),
		)
	}()
	if r.Context().Err() != nil {
		stop("client_disconnect")
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	controller.SetWriteDeadline(idleDeadline)
	if err := controller.Flush(); err != nil {
		writeFailed()
		return
	}

	buffer := make([]byte, 32<<10)
	var boundaries eventBoundaries
	for {
		remaining := stream.limit - count
		n, readErr := stream.body.Read(buffer[:min(int64(len(buffer)), remaining+1)])
		if r.Context().Err() != nil {
			stop("client_disconnect")
			return
		}
		if reason.Load() != nil {
			return
		}
		oversize := int64(n) > remaining
		n = int(min(int64(n), remaining))
		if n > 0 {
			if boundaries.scan(buffer[:n]) {
				idleDeadline = time.Now().Add(stream.idle)
				timer.Reset(stream.idle)
			}
			controller.SetWriteDeadline(idleDeadline)
			// Preserve SSE bytes. The fixed media type and nosniff prevent HTML interpretation.
			// nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter
			written, err := w.Write(buffer[:n])
			digest.Write(buffer[:written])
			count += int64(written)
			if err != nil || written != n {
				writeFailed()
				return
			}
			if err := controller.Flush(); err != nil {
				writeFailed()
				return
			}
		}
		if oversize {
			stop("max_stream_bytes")
			return
		}
		if readErr != nil {
			if readErr != io.EOF {
				stop("upstream_read_error")
			}
			return
		}
	}
}

// eventBoundaries recognizes blank lines across chunks, including CRLF and bare CR.
type eventBoundaries struct {
	lineData bool
	lastCR   bool
}

func (e *eventBoundaries) scan(chunk []byte) bool {
	boundary := false
	for _, c := range chunk {
		if c == '\n' && e.lastCR {
			e.lastCR = false
			continue
		}
		if c == '\r' || c == '\n' {
			boundary = boundary || !e.lineData
			e.lineData = false
			e.lastCR = c == '\r'
		} else {
			e.lineData = true
			e.lastCR = false
		}
	}
	return boundary
}
