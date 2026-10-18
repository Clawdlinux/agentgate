package advisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
	"unicode/utf8"
)

var ErrInputRejected = errors.New("advisor: input rejected")
var ErrUnavailable = errors.New("advisor: unavailable")

const MaxParamsBytes = 256 << 10

// MarshalJSON avoids HTML expansion. The advisor input is JSON, not an HTML document.
func MarshalJSON(value any) ([]byte, error) {
	var out bytes.Buffer
	encoder := json.NewEncoder(&out)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(out.Bytes(), []byte("\n")), nil
}

// PrepareState preserves a bounded JSON prefix when complete params cannot fit.
// Truncation always requires approval, even when the returned probabilities look safe.
func PrepareState(state State) (State, bool, error) {
	params, exists := state["params"]
	if !exists {
		return state, false, nil
	}
	encoded, err := MarshalJSON(params)
	if err != nil {
		return nil, false, errors.Join(ErrInputRejected, err)
	}
	if len(encoded) <= MaxParamsBytes {
		return state, false, nil
	}
	prefix := encoded[:MaxParamsBytes/2-64]
	for !utf8.Valid(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	prepared := make(State, len(state))
	for k, v := range state {
		prepared[k] = v
	}
	prepared["params"] = map[string]any{"_truncated_json": string(prefix)}
	return prepared, true, nil
}

func IsUnavailable(err error) bool {
	if errors.Is(err, ErrInputRejected) {
		return false
	}
	if errors.Is(err, ErrUnavailable) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var networkError net.Error
	return errors.As(err, &networkError)
}

func rejected(message string) error {
	return errors.Join(ErrInputRejected, errors.New(strings.TrimSpace(message)))
}
