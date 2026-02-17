package testrunner

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type TestClient struct {
	baseURL    string
	httpClient *http.Client
	jwtToken   string
	apiKey     string
}

func NewTestClient(baseURL string) *TestClient {
	return &TestClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (c *TestClient) WithJWT(token string) *TestClient {
	return &TestClient{
		baseURL:    c.baseURL,
		httpClient: c.httpClient,
		jwtToken:   token,
		apiKey:     c.apiKey,
	}
}

func (c *TestClient) WithAPIKey(key string) *TestClient {
	return &TestClient{
		baseURL:    c.baseURL,
		httpClient: c.httpClient,
		jwtToken:   c.jwtToken,
		apiKey:     key,
	}
}

func (c *TestClient) GET(path string) (*http.Response, error) {
	req, err := http.NewRequest(http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}

	c.setAuthHeaders(req)
	return c.httpClient.Do(req)
}

func (c *TestClient) POST(path string, body interface{}) (*http.Response, error) {
	var bodyReader io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, bodyReader)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Content-Type", "application/json")
	c.setAuthHeaders(req)
	return c.httpClient.Do(req)
}

func (c *TestClient) setAuthHeaders(req *http.Request) {
	if c.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+c.apiKey)
	} else if c.jwtToken != "" {
		req.Header.Set("Authorization", "Bearer "+c.jwtToken)
	}
}

func (c *TestClient) WaitForHealth(timeout time.Duration) error {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		resp, err := c.GET("/plugins")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		time.Sleep(1 * time.Second)
	}

	return fmt.Errorf("verifier not healthy after %v timeout", timeout)
}

func ReadJSONResponse(resp *http.Response, v interface{}) error {
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read response body: %w", err)
	}

	err = json.Unmarshal(body, v)
	if err != nil {
		return fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return nil
}
