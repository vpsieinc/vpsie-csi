package govpsie

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
)

const (
	libraryVersion = "2.0"
	defaultBaseURL = "https://api.vpsie.com/apps/v2"
	userAgent      = "vpsiecli/" + libraryVersion
	mediaType      = "application/json"
)

type Client struct {
	// HTTP client used to communicate with the VPSIE API.
	client *http.Client

	// Base URL for API requests.
	BaseURL *url.URL

	// User agent for client
	UserAgent string
	headers   map[string]string

	// services
	Account  AccountService
	Storage  StorageService
	Snapshot SnapshotService
	Vpsie    VpsieService
}

type ErrorRsp struct {
	Error   bool   `json:"error"`
	Code    int    `json:"code"`
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

type GeneralRspRoot struct {
	Error bool        `json:"error"`
	Data  interface{} `json:"data"`
}

// ListOptions specifies the optional parameters to various List methods that support pagination.
type ListOptions struct {
	// For paginated result sets, page of results to retrieve.
	Page int `url:"page,omitempty"`

	// For paginated result sets, the number of results to include per page.
	PerPage int `url:"per_page,omitempty"`
}

func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	baseURL, _ := url.Parse(defaultBaseURL)

	c := &Client{
		client:    httpClient,
		BaseURL:   baseURL,
		UserAgent: userAgent,
	}

	c.Account = &accountServiceHandler{client: c}
	c.Storage = &storageServiceHandler{client: c}
	c.Snapshot = &snapshotServiceHandler{client: c}
	c.Vpsie = &vpsieServiceHandler{client: c}

	c.headers = make(map[string]string)
	return c
}

func (c *Client) SetRequestHeaders(headers map[string]string) {

	for k, v := range headers {
		c.headers[k] = v
	}
}

// SetUserAgent Overrides the default UserAgent
func (c *Client) SetUserAgent(ua string) {
	c.UserAgent = ua
}

// SetBaseURL Overrides the default BaseUrl
func (c *Client) SetBaseURL(baseURL string) error {
	updatedURL, err := url.Parse(baseURL)

	if err != nil {
		return err
	}

	c.BaseURL = updatedURL
	return nil
}

// value pointed to by body is JSON encoded and included in as the request body.
func (c *Client) NewRequest(ctx context.Context, method, urlStr string, body interface{}) (*http.Request, error) {
	u, err := c.BaseURL.Parse(urlStr)
	if err != nil {
		return nil, err
	}

	var req *http.Request
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		req, err = http.NewRequest(method, u.String(), nil)
		if err != nil {
			return nil, err
		}

	default:
		buf := new(bytes.Buffer)
		if body != nil {
			err = json.NewEncoder(buf).Encode(body)
			if err != nil {
				return nil, err
			}
		}

		req, err = http.NewRequest(method, u.String(), buf)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", mediaType)
	}

	for k, v := range c.headers {
		req.Header.Add(k, v)
	}

	req.Header.Set("Accept", mediaType)
	req.Header.Set("User-Agent", c.UserAgent)

	return req, nil
}

func (c *Client) Do(ctx context.Context, req *http.Request, v interface{}) error {

	req = req.WithContext(ctx)
	res, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer res.Body.Close()

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return err
	}

	if res.StatusCode < http.StatusOK || res.StatusCode >= 300 {
		var errRsp ErrorRsp
		if err := json.Unmarshal(body, &errRsp); err != nil {
			return err
		}

		return errors.New(errRsp.Message)
	}

	if v != nil {
		if err := json.Unmarshal(body, v); err != nil {
			return err
		}
	}

	return nil
}

// StreamToString converts a reader to a string
func StreamToString(stream io.Reader) string {
	buf := new(bytes.Buffer)
	_, _ = buf.ReadFrom(stream)
	return buf.String()
}
