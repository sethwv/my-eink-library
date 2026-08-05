// Package hardcover is a minimal client for the Hardcover GraphQL API
// (https://docs.hardcover.app/api/), used to fill in and clean up book
// metadata (series, release date, title) that a library's EPUB files often
// don't have or get wrong.
package hardcover

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// endpoint is a var (not const) only so tests can point it at an
// httptest.Server instead of the real API.
var endpoint = "https://api.hardcover.app/v1/graphql"

// Client is safe for concurrent use — every call goes through a shared rate
// limiter (Hardcover's own limit is 60 requests/min) regardless of which
// goroutine (the background enrichment queue, or a manual per-book check)
// is asking, so nothing needs its own separate throttling logic.
type Client struct {
	token string
	http  *http.Client

	mu           sync.Mutex
	lastCallTime time.Time
}

// minInterval is slightly more than 1/60th of a minute, for a safety margin
// under Hardcover's 60 req/min limit.
const minInterval = 1100 * time.Millisecond

// New creates a Client using the given API bearer token (from
// https://hardcover.app/settings, per Hardcover's own docs).
func New(token string) *Client {
	return &Client{
		token: token,
		http:  &http.Client{Timeout: 30 * time.Second},
	}
}

// Enabled reports whether a token was configured at all — callers should
// skip enrichment entirely (not error) when this is false, since Hardcover
// integration is optional.
func (c *Client) Enabled() bool {
	return c != nil && c.token != ""
}

type graphqlRequest struct {
	Query     string `json:"query"`
	Variables any    `json:"variables,omitempty"`
}

type graphqlError struct {
	Message string `json:"message"`
}

func (c *Client) do(ctx context.Context, query string, variables any, out any) error {
	c.throttle()

	body, err := json.Marshal(graphqlRequest{Query: query, Variables: variables})
	if err != nil {
		return fmt.Errorf("hardcover: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("hardcover: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+c.token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("hardcover: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("hardcover: unexpected status %d", resp.StatusCode)
	}

	var envelope struct {
		Errors []graphqlError  `json:"errors"`
		Data   json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&envelope); err != nil {
		return fmt.Errorf("hardcover: decode response: %w", err)
	}
	if len(envelope.Errors) > 0 {
		return fmt.Errorf("hardcover: %s", envelope.Errors[0].Message)
	}
	if out != nil {
		if err := json.Unmarshal(envelope.Data, out); err != nil {
			return fmt.Errorf("hardcover: decode data: %w", err)
		}
	}
	return nil
}

// throttle blocks until it's been at least minInterval since the previous
// call, serializing every request through this client regardless of caller.
func (c *Client) throttle() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if wait := minInterval - time.Since(c.lastCallTime); wait > 0 {
		time.Sleep(wait)
	}
	c.lastCallTime = time.Now()
}
