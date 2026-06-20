package qdrant

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    *url.URL
	httpClient *http.Client
}

type Point struct {
	ID      string         `json:"id"`
	Vector  []float64      `json:"vector"`
	Payload map[string]any `json:"payload"`
}

type SearchResult struct {
	ID      any            `json:"id"`
	Score   float64        `json:"score"`
	Payload map[string]any `json:"payload"`
}

func New(baseURL string, options ...Option) (*Client, error) {
	if strings.TrimSpace(baseURL) == "" {
		return nil, fmt.Errorf("qdrant base URL is required")
	}

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("parse qdrant base URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("qdrant base URL must include scheme and host")
	}

	client := &Client{
		baseURL: parsed,
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
	for _, option := range options {
		option(client)
	}
	if client.httpClient == nil {
		client.httpClient = http.DefaultClient
	}
	return client, nil
}

type Option func(*Client)

func WithHTTPClient(httpClient *http.Client) Option {
	return func(client *Client) {
		client.httpClient = httpClient
	}
}

func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/").String(), nil)
	if err != nil {
		return fmt.Errorf("create health request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("qdrant health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("qdrant health status %d", resp.StatusCode)
	}
	return nil
}

func (c *Client) EnsureCollection(ctx context.Context, collection string, vectorSize int) error {
	if strings.TrimSpace(collection) == "" {
		return fmt.Errorf("collection is required")
	}
	if vectorSize <= 0 {
		return fmt.Errorf("vector size must be positive")
	}

	path := collectionPath(collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint(path).String(), nil)
	if err != nil {
		return fmt.Errorf("create collection check request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("check collection: %w", err)
	}
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return nil
	}
	if resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("check collection status %d", resp.StatusCode)
	}

	payload := map[string]any{
		"vectors": map[string]any{
			"size":     vectorSize,
			"distance": "Cosine",
		},
	}
	if err := c.doJSON(ctx, http.MethodPut, path, payload, nil); err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	return nil
}

func (c *Client) RecreateCollection(ctx context.Context, collection string, vectorSize int) error {
	if strings.TrimSpace(collection) == "" {
		return fmt.Errorf("collection is required")
	}
	if vectorSize <= 0 {
		return fmt.Errorf("vector size must be positive")
	}

	if err := c.deleteCollection(ctx, collection); err != nil {
		return err
	}
	return c.EnsureCollection(ctx, collection, vectorSize)
}

func (c *Client) Upsert(ctx context.Context, collection string, points []Point) error {
	if len(points) == 0 {
		return nil
	}
	payload := map[string]any{"points": points}
	path := collectionPath(collection) + "/points?wait=true"
	if err := c.doJSON(ctx, http.MethodPut, path, payload, nil); err != nil {
		return fmt.Errorf("upsert points: %w", err)
	}
	return nil
}

func (c *Client) Search(ctx context.Context, collection string, vector []float64, limit int) ([]SearchResult, error) {
	if len(vector) == 0 {
		return nil, fmt.Errorf("query vector is empty")
	}
	if limit <= 0 {
		limit = 8
	}
	payload := map[string]any{
		"vector":       vector,
		"limit":        limit,
		"with_payload": true,
	}
	var response struct {
		Result []SearchResult `json:"result"`
	}
	path := collectionPath(collection) + "/points/search"
	if err := c.doJSON(ctx, http.MethodPost, path, payload, &response); err != nil {
		return nil, fmt.Errorf("search points: %w", err)
	}
	return response.Result, nil
}

func (c *Client) deleteCollection(ctx context.Context, collection string) error {
	path := collectionPath(collection)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.endpoint(path).String(), nil)
	if err != nil {
		return fmt.Errorf("create delete collection request: %w", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("delete collection: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusNotFound {
		return nil
	}
	return fmt.Errorf("delete collection status %d", resp.StatusCode)
}

func (c *Client) doJSON(ctx context.Context, method string, path string, payload any, response any) error {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.endpoint(path).String(), body)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("Accept", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("%s %s: %w", method, path, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		errorBody, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("%s %s: status %d: %s", method, path, resp.StatusCode, strings.TrimSpace(string(errorBody)))
	}

	if response == nil {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(response); err != nil {
		return fmt.Errorf("decode response: %w", err)
	}
	return nil
}

func (c *Client) endpoint(path string) *url.URL {
	ref := &url.URL{Path: path}
	if strings.Contains(path, "?") {
		parts := strings.SplitN(path, "?", 2)
		ref.Path = parts[0]
		ref.RawQuery = parts[1]
	}
	return c.baseURL.ResolveReference(ref)
}

func collectionPath(collection string) string {
	return "/collections/" + url.PathEscape(collection)
}
