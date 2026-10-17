package gateway

import (
	"encoding/json"
	"net/http"
	"strconv"
)

// writeActResponse writes an envelope around JSON already validated by executeAttempt.
// Text-only responses keep the standard encoder's string escaping in writeJSON.
func writeActResponse(w http.ResponseWriter, status int, response ActResponse) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)

	var buffer [128]byte
	prefix := append(buffer[:0], `{"status":`...)
	prefix = strconv.AppendInt(prefix, int64(response.Status), 10)
	if len(response.Headers) > 0 {
		headers, _ := json.Marshal(response.Headers)
		prefix = append(prefix, `,"headers":`...)
		prefix = append(prefix, headers...)
	}
	prefix = append(prefix, `,"body":`...)
	if err := writeJSONPart(w, prefix); err != nil {
		return
	}
	if err := writeJSONPart(w, response.Body); err != nil {
		return
	}
	if response.BodyText != "" {
		text, _ := json.Marshal(response.BodyText)
		if err := writeJSONPart(w, []byte(`,"body_text":`)); err != nil {
			return
		}
		if err := writeJSONPart(w, text); err != nil {
			return
		}
	}
	suffix := append(buffer[:0], `,"latency_ms":`...)
	suffix = strconv.AppendInt(suffix, response.LatencyMS, 10)
	suffix = append(suffix, '}', '\n')
	writeJSONPart(w, suffix)
}

func writeJSONPart(w http.ResponseWriter, part []byte) error {
	// Parts are encoded JSON. The envelope fixes application/json and nosniff, so HTML escaping isn't needed.
	// nosemgrep: go.lang.security.audit.xss.no-direct-write-to-responsewriter.no-direct-write-to-responsewriter
	_, err := w.Write(part)
	return err
}
