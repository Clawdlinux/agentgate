package gateway

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestWriteActResponseFieldOrderAndOmission(t *testing.T) {
	for _, response := range []ActResponse{
		{Status: 202, Body: json.RawMessage(`{"ok":true}`), LatencyMS: 42},
		{Status: 202, Headers: map[string]string{"x-name": "<name>&"}, Body: json.RawMessage(`null`), LatencyMS: 42},
		{Status: 202, Headers: map[string]string{"large": strings.Repeat("x", 256)}, Body: json.RawMessage(`[]`)},
		{Status: 202, Headers: map[string]string{}, Body: json.RawMessage(`"string"`), BodyText: "<text>\n"},
		{Status: 202, BodyText: "<text>\n", LatencyMS: 42},
		{Status: 202},
	} {
		var legacy bytes.Buffer
		if err := json.NewEncoder(&legacy).Encode(response); err != nil {
			t.Fatal(err)
		}
		w := httptest.NewRecorder()
		writeJSON(w, 201, response)
		if w.Code != 201 || w.Header().Get("Content-Type") != "application/json" || w.Body.String() != legacy.String() {
			t.Fatalf("status=%d headers=%v actual=%q legacy=%q", w.Code, w.Header(), w.Body.String(), legacy.String())
		}
		if len(response.Body) > 0 && w.Header().Get("X-Content-Type-Options") != "nosniff" {
			t.Fatal("raw JSON response missing nosniff")
		}
	}
}
