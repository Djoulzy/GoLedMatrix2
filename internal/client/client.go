// Package client implements the Go client for the versioned HTTP protocol.
package client

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

	"github.com/Djoulzy/GoLedMatrix2/internal/animation"
	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/server"
)

type Client struct {
	baseURL *url.URL
	http    *http.Client
}

func New(rawURL string, timeout time.Duration) (*Client, error) {
	baseURL, err := url.Parse(rawURL)
	if err != nil {
		return nil, fmt.Errorf("parse server URL: %w", err)
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("server URL must use http or https")
	}
	if baseURL.Host == "" {
		return nil, fmt.Errorf("server URL must include a host")
	}
	baseURL.Path = strings.TrimRight(baseURL.Path, "/")
	return &Client{baseURL: baseURL, http: &http.Client{Timeout: timeout}}, nil
}

func (c *Client) Info(ctx context.Context) (server.Info, error) {
	var info server.Info
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/v1/info"), nil)
	if err != nil {
		return info, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return info, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return info, responseError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&info); err != nil {
		return info, fmt.Errorf("decode server info: %w", err)
	}
	return info, nil
}

func (c *Client) Send(ctx context.Context, next frame.Frame) (uint64, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.endpoint("/v1/frame"), bytes.NewReader(next.Pixels))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", frame.MediaType)
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return 0, responseError(resp)
	}
	var accepted struct {
		Sequence uint64 `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&accepted); err != nil {
		return 0, fmt.Errorf("decode server response: %w", err)
	}
	return accepted.Sequence, nil
}

func (c *Client) DisplayInfo(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/display-info"), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return responseError(resp)
	}
	return nil
}

func (c *Client) DisplayClock(ctx context.Context, mode, color1, color2 string) error {
	query := url.Values{"mode": []string{mode}}
	if color1 != "" {
		query.Set("color1", color1)
	}
	if color2 != "" {
		query.Set("color2", color2)
	}
	endpoint := c.endpoint("/v1/clock") + "?" + query.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return responseError(resp)
	}
	return nil
}

func (c *Client) UploadAnimation(
	ctx context.Context,
	name string,
	bundle animation.Bundle,
	play bool,
) (animation.Metadata, error) {
	var metadata animation.Metadata
	if err := animation.ValidateName(name); err != nil {
		return metadata, err
	}
	reader, writer := io.Pipe()
	go func() {
		err := animation.Encode(writer, bundle)
		_ = writer.CloseWithError(err)
	}()
	endpoint := c.endpoint("/v1/animations/" + name)
	if !play {
		endpoint += "?play=false"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, reader)
	if err != nil {
		return metadata, err
	}
	req.Header.Set("Content-Type", animation.MediaType)
	resp, err := c.http.Do(req)
	if err != nil {
		return metadata, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return metadata, responseError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return metadata, fmt.Errorf("decode animation response: %w", err)
	}
	return metadata, nil
}

func (c *Client) PlayAnimation(ctx context.Context, name string) (animation.Metadata, error) {
	var metadata animation.Metadata
	if err := animation.ValidateName(name); err != nil {
		return metadata, err
	}
	req, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.endpoint("/v1/animations/"+name+"/play"), nil,
	)
	if err != nil {
		return metadata, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return metadata, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusAccepted {
		return metadata, responseError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return metadata, fmt.Errorf("decode animation response: %w", err)
	}
	return metadata, nil
}

func (c *Client) endpoint(path string) string {
	copyOfURL := *c.baseURL
	copyOfURL.Path += path
	return copyOfURL.String()
}

func responseError(resp *http.Response) error {
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("server returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
}
