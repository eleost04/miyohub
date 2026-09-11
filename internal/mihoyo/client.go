package mihoyo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const DefaultMobileUA = "Mozilla/5.0 (Linux; Android 12; Unspecified Device) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/103.0.5060.129 Mobile Safari/537.36 miHoYoBBS/2.106.2"

type Client struct {
	HTTP      *http.Client
	BaseURL   string
	UserAgent string
}

func NewClient(baseURL string) *Client {
	return &Client{HTTP: &http.Client{Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, BaseURL: strings.TrimRight(baseURL, "/"), UserAgent: DefaultMobileUA}
}

func (c *Client) JSON(ctx context.Context, method, path string, query url.Values, body any, headers http.Header, out any) error {
	_, err := c.JSONWithHeaders(ctx, method, path, query, body, headers, out)
	return err
}

// JSONWithHeaders also returns the upstream headers used for clock and login verification.
func (c *Client) JSONWithHeaders(ctx context.Context, method, path string, query url.Values, body any, headers http.Header, out any) (http.Header, error) {
	var reader io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		reader = strings.NewReader(string(raw))
	}
	request, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("User-Agent", c.UserAgent)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	for key, values := range headers {
		request.Header.Del(key)
		for _, value := range values {
			request.Header.Add(key, value)
		}
	}
	if query != nil {
		merged := request.URL.Query()
		for key, values := range query {
			merged[key] = values
		}
		request.URL.RawQuery = merged.Encode()
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return nil, SafeNetworkError(err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return response.Header, &HTTPStatusError{StatusCode: response.StatusCode, RetryAfter: response.Header.Get("Retry-After")}
	}
	if out == nil {
		return response.Header, nil
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, (4<<20)+1))
	if err != nil {
		return response.Header, SafeNetworkError(err)
	}
	if len(raw) > 4<<20 {
		return response.Header, errors.New("上游响应超过大小限制")
	}
	if err := json.Unmarshal(raw, out); err != nil {
		return response.Header, errors.New("上游返回了无效 JSON")
	}
	return response.Header, nil
}

// Carries status metadata without retaining response bodies or request URLs.
// Retry policy belongs to callers, since some GET endpoints have side effects.
type HTTPStatusError struct {
	StatusCode int
	RetryAfter string
}

func (e *HTTPStatusError) Error() string { return fmt.Sprintf("mihoyo http %d", e.StatusCode) }

// URL errors include query-string credentials; keep only the underlying cause.
func SafeNetworkError(err error) error {
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		return fmt.Errorf("上游请求失败: %w", urlErr.Err)
	}
	return fmt.Errorf("上游请求失败: %w", err)
}
