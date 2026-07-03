// Package plane is the adapter to a self-hosted Plane instance (REST API v1).
// It creates/updates work items (issues) and reads their workflow states.
package plane

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config holds the connection details for a Plane project.
type Config struct {
	BaseURL       string // e.g. https://planner.webcloster.cloud
	Token         string // X-API-Key
	WorkspaceSlug string // cannot be inferred via API — asked from the user
	ProjectID     string
}

// Client talks to the Plane REST API.
type Client struct {
	cfg  Config
	http *http.Client
}

// New builds a Plane client.
func New(cfg Config) *Client {
	return &Client{cfg: cfg, http: &http.Client{Timeout: 30 * time.Second}}
}

// Configured reports whether every required connection field is present.
func (c *Client) Configured() bool {
	return c.cfg.BaseURL != "" && c.cfg.Token != "" && c.cfg.WorkspaceSlug != "" && c.cfg.ProjectID != ""
}

// IssueInput is the subset of Plane issue fields we set.
type IssueInput struct {
	Name        string
	Description string   // plain text; wrapped as description_html (used when HTML is empty)
	HTML        string   // pre-rendered rich-text body; takes precedence over Description
	StartDate   string   // YYYY-MM-DD or ""
	TargetDate  string   // YYYY-MM-DD or "" (Plane's "Due date")
	StateID     string   // optional Plane state uuid
	Priority    string   // urgent | high | medium | low | none; "" leaves it untouched
	Labels      []string // label uuids to attach
	Estimate    string   // estimate_point; "" leaves it untouched
}

// Label is a Plane issue label (tag).
type Label struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// IssueRef identifies a work item after creation: its uuid and the project
// sequence number (the "#343" shown in Plane, e.g. LIQMSTR-343).
type IssueRef struct {
	ID  string
	Seq int
}

// State is a Plane workflow state.
type State struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Group string `json:"group"`
}

func (c *Client) base() string {
	return fmt.Sprintf("%s/api/v1/workspaces/%s/projects/%s",
		strings.TrimRight(c.cfg.BaseURL, "/"), c.cfg.WorkspaceSlug, c.cfg.ProjectID)
}

func (in IssueInput) body() map[string]any {
	b := map[string]any{"name": in.Name}
	switch {
	case in.HTML != "":
		b["description_html"] = in.HTML
	case in.Description != "":
		b["description_html"] = "<p>" + html.EscapeString(in.Description) + "</p>"
	}
	if in.StartDate != "" {
		b["start_date"] = in.StartDate
	}
	if in.TargetDate != "" {
		b["target_date"] = in.TargetDate
	}
	if in.StateID != "" {
		b["state"] = in.StateID
	}
	if in.Priority != "" {
		b["priority"] = in.Priority
	}
	if len(in.Labels) > 0 {
		b["labels"] = in.Labels
	}
	if in.Estimate != "" {
		b["estimate_point"] = in.Estimate
	}
	return b
}

// CreateIssue creates a work item and returns its id and sequence number.
func (c *Client) CreateIssue(ctx context.Context, in IssueInput) (IssueRef, error) {
	var out struct {
		ID         string `json:"id"`
		SequenceID int    `json:"sequence_id"`
	}
	if err := c.do(ctx, http.MethodPost, c.base()+"/issues/", in.body(), &out); err != nil {
		return IssueRef{}, err
	}
	if out.ID == "" {
		return IssueRef{}, fmt.Errorf("plane: create issue returned no id")
	}
	return IssueRef{ID: out.ID, Seq: out.SequenceID}, nil
}

// UpdateIssue patches an existing work item.
func (c *Client) UpdateIssue(ctx context.Context, id string, in IssueInput) error {
	return c.do(ctx, http.MethodPatch, c.base()+"/issues/"+id+"/", in.body(), nil)
}

// DeleteIssue removes a work item from Plane.
func (c *Client) DeleteIssue(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, c.base()+"/issues/"+id+"/", nil, nil)
}

// ListStates returns the project's workflow states.
func (c *Client) ListStates(ctx context.Context) ([]State, error) {
	var out struct {
		Results []State `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, c.base()+"/states/", nil, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// ListLabels returns the project's issue labels (tags).
func (c *Client) ListLabels(ctx context.Context) ([]Label, error) {
	var out struct {
		Results []Label `json:"results"`
	}
	if err := c.do(ctx, http.MethodGet, c.base()+"/labels/", nil, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// IssueStateID returns the state id currently set on a work item.
func (c *Client) IssueStateID(ctx context.Context, id string) (string, error) {
	var out struct {
		State string `json:"state"`
	}
	if err := c.do(ctx, http.MethodGet, c.base()+"/issues/"+id+"/", nil, &out); err != nil {
		return "", err
	}
	return out.State, nil
}

func (c *Client) do(ctx context.Context, method, url string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, reader)
	if err != nil {
		return err
	}
	req.Header.Set("X-API-Key", c.cfg.Token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return fmt.Errorf("plane: %s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(data)))
	}
	if out != nil && len(data) > 0 {
		if err := json.Unmarshal(data, out); err != nil {
			return fmt.Errorf("plane: decode: %w", err)
		}
	}
	return nil
}
