// Package client is a minimal HTTP client for the pfwebd REST API.
package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Client struct {
	endpoint string
	token    string
	hc       *http.Client
}

func New(endpoint, token string) *Client {
	return &Client{
		endpoint: strings.TrimRight(endpoint, "/"),
		token:    token,
		hc:       &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.endpoint+path, rd)
	if err != nil {
		return err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := c.hc.Do(req)
	if err != nil {
		return fmt.Errorf("pfwebd API %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		var ae struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(data, &ae) == nil && ae.Error != "" {
			return fmt.Errorf("pfwebd API %s %s: %s (HTTP %d)", method, path, ae.Error, res.StatusCode)
		}
		return fmt.Errorf("pfwebd API %s %s: HTTP %d", method, path, res.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// --- Anchor ruleset ---

type Pending struct {
	Rules    []string  `json:"rules"`
	Deadline time.Time `json:"deadline"`
}

type AnchorState struct {
	Active  []string `json:"active"`
	Pending *Pending `json:"pending"`
	Live    string   `json:"live"`
}

func (c *Client) GetAnchor(ctx context.Context) (AnchorState, error) {
	var st AnchorState
	err := c.do(ctx, http.MethodGet, "/api/anchor", nil, &st)
	return st, err
}

func (c *Client) ApplyAnchor(ctx context.Context, rules []string) (AnchorState, error) {
	var st AnchorState
	err := c.do(ctx, http.MethodPost, "/api/anchor", map[string][]string{"rules": rules}, &st)
	return st, err
}

func (c *Client) ConfirmAnchor(ctx context.Context) (AnchorState, error) {
	var st AnchorState
	err := c.do(ctx, http.MethodPost, "/api/anchor/confirm", struct{}{}, &st)
	return st, err
}

func (c *Client) CancelAnchor(ctx context.Context) error {
	return c.do(ctx, http.MethodPost, "/api/anchor/cancel", struct{}{}, nil)
}

// --- PF tables ---

type Table struct {
	Name      string   `json:"name"`
	Writable  bool     `json:"writable"`
	Addresses []string `json:"addresses"`
}

func (c *Client) GetTable(ctx context.Context, name string) (Table, error) {
	var t Table
	err := c.do(ctx, http.MethodGet, "/api/tables/"+name, nil, &t)
	return t, err
}

func (c *Client) tableMutate(ctx context.Context, name, action, address string) error {
	body := map[string]string{"action": action, "address": address}
	return c.do(ctx, http.MethodPost, "/api/tables/"+name, body, nil)
}

func (c *Client) TableAdd(ctx context.Context, name, address string) error {
	return c.tableMutate(ctx, name, "add", address)
}

func (c *Client) TableDelete(ctx context.Context, name, address string) error {
	return c.tableMutate(ctx, name, "delete", address)
}

// --- Status ---

type Status struct {
	Enabled bool   `json:"enabled"`
	Since   string `json:"since"`
}

func (c *Client) GetStatus(ctx context.Context) (Status, error) {
	var st Status
	err := c.do(ctx, http.MethodGet, "/api/status", nil, &st)
	return st, err
}
