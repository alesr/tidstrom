package exporter

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"

	"github.com/alesr/tidstrom/streambuffer"
)

// Error is a custom error type.
type Error struct {
	Message string `json:"message"`
}

// Error implements the error interface.
func (e Error) Error() string {
	return e.Message
}

var errInputChannelClosed = Error{Message: "input channel closed"}

// Exporter is a struct that exports snapshots to a remote endpoint.
type Exporter struct {
	baseURL *url.URL
	cli     *http.Client
	inputCh <-chan *streambuffer.Snapshot
}

// NewExporter creates a new Exporter instance.
func NewExporter(baseURL string, httpCli *http.Client, inputCh <-chan *streambuffer.Snapshot) (*Exporter, error) {
	if baseURL == "" || httpCli == nil || inputCh == nil {
		return nil, errors.New("invalid arguments")
	}
	u, err := url.Parse(baseURL)
	if err != nil {
		return nil, fmt.Errorf("invalid base URL: %w", err)
	}
	return &Exporter{
		baseURL: u,
		cli:     httpCli,
		inputCh: inputCh,
	}, nil
}

// Run starts the exporter.
func (e *Exporter) Run(ctx context.Context) error {
	select {
	case snapshot, ok := <-e.inputCh:
		if !ok {
			return errInputChannelClosed
		}
		return e.send(ctx, snapshot)
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Send sends a snapshot to the remote endpoint.
func (e *Exporter) send(ctx context.Context, snapshot *streambuffer.Snapshot) error {
	u := *e.baseURL
	endpoint, err := url.JoinPath(u.String(), "snapshot")
	if err != nil {
		return fmt.Errorf("invalid base URL: %w", err)
	}

	b, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("could not marshal snapshot: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("could not create request: %w", err)
	}

	resp, err := e.cli.Do(req)
	if err != nil {
		return fmt.Errorf("could not send request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}
