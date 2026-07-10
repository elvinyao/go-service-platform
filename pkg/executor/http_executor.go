package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/elvinyao/go-service-platform/pkg/ruleengine"
)

type HTTPExecutor struct {
	client *http.Client
}

func NewHTTPExecutor() *HTTPExecutor {
	return &HTTPExecutor{}
}

// NewHTTPExecutorWithClient creates an executor with an application-owned HTTP client.
func NewHTTPExecutorWithClient(client *http.Client) *HTTPExecutor {
	return &HTTPExecutor{client: client}
}

func (e *HTTPExecutor) Type() string {
	return "http"
}

func (e *HTTPExecutor) Execute(ctx context.Context, msg ruleengine.Message, action ruleengine.Action) error {
	if ctx == nil {
		ctx = context.Background()
	}
	method, _ := action.Params["method"].(string)
	if method == "" {
		method = http.MethodPost
	}

	url, _ := action.Params["url"].(string)
	if url == "" {
		return fmt.Errorf("http action requires params.url")
	}

	body := ""
	if tmpl, _ := action.Params["body_template"].(string); tmpl != "" {
		rendered, err := RenderTemplate(tmpl, msg)
		if err != nil {
			return fmt.Errorf("render http body template: %w", err)
		}
		body = rendered
	}

	timeout := 3 * time.Second
	if timeoutMS, ok := action.Params["timeout_ms"]; ok {
		switch v := timeoutMS.(type) {
		case int:
			if v > 0 {
				timeout = time.Duration(v) * time.Millisecond
			}
		case int64:
			if v > 0 {
				timeout = time.Duration(v) * time.Millisecond
			}
		case float64:
			if v > 0 {
				timeout = time.Duration(v) * time.Millisecond
			}
		}
	}

	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(requestCtx, method, url, bytes.NewBufferString(body))
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	if headers, ok := action.Params["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			req.Header.Set(k, fmt.Sprintf("%v", v))
		}
	}
	if req.Header.Get("Content-Type") == "" {
		req.Header.Set("Content-Type", "application/json")
	}

	client := e.client
	if client == nil {
		client = &http.Client{Timeout: timeout}
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}

	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
	return fmt.Errorf("http executor non-2xx status=%d body=%s", resp.StatusCode, string(b))
}
