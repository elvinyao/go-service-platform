package executor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"project/internal/model"
	"project/internal/ruleengine"
)

type HTTPExecutor struct{}

func NewHTTPExecutor() *HTTPExecutor {
	return &HTTPExecutor{}
}

func (e *HTTPExecutor) Type() string {
	return "http"
}

func (e *HTTPExecutor) Execute(ctx context.Context, msg model.Message, action ruleengine.Action) error {
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
		rendered, err := renderTemplate(tmpl, msg)
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

	client := &http.Client{Timeout: timeout}
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
