package oauth

import (
	"bytes"
	"strings"
	"testing"
)

func TestLinkedPageEscapesService(t *testing.T) {
	var body bytes.Buffer
	if err := linkedPage.Execute(&body, "<script>alert(1)</script>"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(body.String(), "<script>") || !strings.Contains(body.String(), "&lt;script&gt;") {
		t.Fatal("callback page did not escape the service name")
	}
}
