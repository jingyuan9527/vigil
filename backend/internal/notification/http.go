package notification

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"time"
)

// postJSON 发送 JSON 载荷到 URL，非 2xx 返回带响应体的错误。
// name 仅用于错误信息标识（如 "wecom"/"feishu"），帮助日志定位。
func postJSON(ctx context.Context, url string, body []byte, name string) error {
	if ctx == nil {
		ctx = context.Background()
	}
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build %s request: %w", name, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("send %s: %w", name, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s returned %d: %s", name, resp.StatusCode, string(b))
	}
	return nil
}
