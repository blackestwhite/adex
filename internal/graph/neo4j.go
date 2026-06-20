package graph

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
	username   string
	password   string
	httpClient *http.Client
}

type Statement struct {
	Statement  string         `json:"statement"`
	Parameters map[string]any `json:"parameters,omitempty"`
}

type Result struct {
	Columns []string
	Rows    [][]any
}

func New(urlText string, username string, password string, options ...Option) (*Client, error) {
	if strings.TrimSpace(urlText) == "" {
		return nil, fmt.Errorf("neo4j URL is required")
	}
	parsed, err := url.Parse(urlText)
	if err != nil {
		return nil, fmt.Errorf("parse neo4j URL: %w", err)
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return nil, fmt.Errorf("neo4j URL must include scheme and host")
	}
	client := &Client{
		baseURL:  parsed,
		username: username,
		password: password,
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
	_, err := c.Run(ctx, []Statement{{Statement: "RETURN 1 AS ok"}})
	return err
}

func (c *Client) Query(ctx context.Context, cypher string, parameters map[string]any) (Result, error) {
	results, err := c.Run(ctx, []Statement{{Statement: cypher, Parameters: parameters}})
	if err != nil {
		return Result{}, err
	}
	if len(results) == 0 {
		return Result{}, nil
	}
	return results[0], nil
}

func (c *Client) Run(ctx context.Context, statements []Statement) ([]Result, error) {
	if len(statements) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(map[string]any{"statements": statements})
	if err != nil {
		return nil, fmt.Errorf("encode cypher request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/db/neo4j/tx/commit").String(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create cypher request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if c.username != "" || c.password != "" {
		req.SetBasicAuth(c.username, c.password)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("neo4j request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, fmt.Errorf("neo4j status %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}

	var decoded struct {
		Results []struct {
			Columns []string `json:"columns"`
			Data    []struct {
				Row []any `json:"row"`
			} `json:"data"`
		} `json:"results"`
		Errors []struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&decoded); err != nil {
		return nil, fmt.Errorf("decode neo4j response: %w", err)
	}
	if len(decoded.Errors) > 0 {
		return nil, fmt.Errorf("neo4j error %s: %s", decoded.Errors[0].Code, decoded.Errors[0].Message)
	}

	results := make([]Result, 0, len(decoded.Results))
	for _, result := range decoded.Results {
		rows := make([][]any, 0, len(result.Data))
		for _, item := range result.Data {
			rows = append(rows, item.Row)
		}
		results = append(results, Result{Columns: result.Columns, Rows: rows})
	}
	return results, nil
}

func (c *Client) endpoint(path string) *url.URL {
	return c.baseURL.ResolveReference(&url.URL{Path: path})
}
