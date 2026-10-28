package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/Djoulzy/GoLedMatrix2/internal/frame"
	"github.com/Djoulzy/GoLedMatrix2/internal/photo"
)

func (c *Client) UploadPhoto(ctx context.Context, name string, next frame.Frame, display bool) (photo.Metadata, error) {
	var metadata photo.Metadata
	if err := photo.ValidateName(name); err != nil {
		return metadata, err
	}
	if _, err := frame.New(next.Width, next.Height, next.Pixels); err != nil {
		return metadata, err
	}
	endpoint := c.endpoint("/v1/photos/" + name)
	if !display {
		endpoint += "?display=false"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(next.Pixels))
	if err != nil {
		return metadata, err
	}
	req.Header.Set("Content-Type", frame.MediaType)
	resp, err := c.http.Do(req)
	if err != nil {
		return metadata, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		return metadata, responseError(resp)
	}
	if err := json.NewDecoder(resp.Body).Decode(&metadata); err != nil {
		return metadata, fmt.Errorf("decode photo response: %w", err)
	}
	return metadata, nil
}

func (c *Client) ListPhotos(ctx context.Context) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint("/v1/photos"), nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, responseError(resp)
	}
	var names []string
	if err := json.NewDecoder(resp.Body).Decode(&names); err != nil {
		return nil, fmt.Errorf("decode photo list: %w", err)
	}
	return names, nil
}

func (c *Client) DisplayPhoto(ctx context.Context, name string) (photo.Metadata, error) {
	var metadata photo.Metadata
	if err := photo.ValidateName(name); err != nil {
		return metadata, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint("/v1/photos/"+name+"/display"), nil)
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
		return metadata, fmt.Errorf("decode photo response: %w", err)
	}
	return metadata, nil
}
