package advisor

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
)

const MaxResponseBytes = 64 << 10
const MaxInputBytes = 1 << 20

type HTTPAdvisor struct {
	endpoint string
	client   *http.Client
}

func NewHTTPAdvisor(rawURL string) (*HTTPAdvisor, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, errors.New("advisor: invalid URL")
	}
	host := u.Hostname()
	cluster := strings.HasSuffix(host, ".svc.cluster.local") && len(host) > len(".svc.cluster.local")
	if (u.Scheme != "http" && u.Scheme != "https") || u.User != nil || u.RawQuery != "" || u.Fragment != "" ||
		(u.Path != "" && u.Path != "/") || (host != "localhost" && host != "127.0.0.1" && host != "::1" && !cluster) {
		return nil, errors.New("advisor: URL must name loopback or a .svc.cluster.local service")
	}
	transport := &http.Transport{
		Proxy:               nil,
		DialContext:         (&net.Dialer{Timeout: Timeout}).DialContext,
		TLSHandshakeTimeout: Timeout, ResponseHeaderTimeout: Timeout,
		MaxIdleConns: 16, MaxIdleConnsPerHost: 16,
	}
	u.Path = "/v1/decide"
	return &HTTPAdvisor{endpoint: u.String(), client: &http.Client{
		Timeout: Timeout, Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}, nil
}

func (h *HTTPAdvisor) CloseIdleConnections() { h.client.CloseIdleConnections() }

func (h *HTTPAdvisor) Decide(ctx context.Context, state State, questions Questions) (Answers, error) {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	state, truncated, err := PrepareState(state)
	if err != nil {
		return Answers{}, err
	}
	payload, err := MarshalJSON(struct {
		State     State     `json:"state"`
		Questions Questions `json:"questions"`
	}{state, questions})
	if err != nil {
		return Answers{Truncated: truncated}, errors.Join(ErrInputRejected, err)
	}
	if len(payload) > MaxInputBytes {
		return Answers{Truncated: truncated}, rejected("advisor: input too large")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, h.endpoint, bytes.NewReader(payload))
	if err != nil {
		return Answers{Truncated: truncated}, errors.Join(ErrInputRejected, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := h.client.Do(req)
	if err != nil {
		return Answers{Truncated: truncated}, errors.Join(ErrUnavailable, err)
	}
	defer resp.Body.Close()
	eligibleUnavailable := resp.StatusCode >= 500 && resp.StatusCode < 600
	if resp.StatusCode >= 300 && resp.StatusCode < 500 {
		return Answers{Truncated: truncated}, rejected(fmt.Sprintf("advisor: HTTP status %d", resp.StatusCode))
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
	if err != nil {
		return Answers{Truncated: truncated}, errors.Join(ErrUnavailable, err)
	}
	if eligibleUnavailable {
		var partial Answers
		if len(body) <= MaxResponseBytes && json.Unmarshal(body, &partial) == nil && partial.Validate() == nil {
			partial.Truncated = truncated
			return partial, errors.Join(ErrUnavailable, fmt.Errorf("advisor: HTTP status %d", resp.StatusCode))
		}
		return Answers{Truncated: truncated}, errors.Join(ErrUnavailable, fmt.Errorf("advisor: HTTP status %d", resp.StatusCode))
	}
	if len(body) > MaxResponseBytes {
		return Answers{Truncated: truncated}, rejected("advisor: response too large")
	}
	var result Answers
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&result); err != nil {
		return Answers{Truncated: truncated}, rejected("advisor: invalid response")
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return Answers{Truncated: truncated}, rejected("advisor: trailing response data")
	}
	if err := result.Validate(); err != nil {
		return Answers{Truncated: truncated}, errors.Join(ErrInputRejected, err)
	}
	if resp.StatusCode != http.StatusOK {
		return Answers{Truncated: truncated}, rejected(fmt.Sprintf("advisor: HTTP status %d", resp.StatusCode))
	}
	result.Truncated = truncated
	if err := ctx.Err(); err != nil {
		return result, errors.Join(ErrUnavailable, err)
	}
	return result, nil
}
