package notification

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// captureRequest 起一个把请求体全部记下来的假端点，返回 URL、请求体读取函数与清理函数。
func captureRequest(t *testing.T) (string, func() (*http.Request, []byte), func()) {
	t.Helper()
	var mu sync.Mutex
	var req *http.Request
	var body []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		req = r.Clone(r.Context())
		req.Body = nil
		body = b
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	return srv.URL,
		func() (*http.Request, []byte) {
			mu.Lock()
			defer mu.Unlock()
			return req, body
		},
		func() { srv.Close() }
}

func TestWebhookProviderJSON(t *testing.T) {
	url, get, cleanup := captureRequest(t)
	defer cleanup()
	cfg, _ := json.Marshal(map[string]interface{}{
		"url":     url,
		"format":  "json",
		"headers": map[string]string{"X-Custom": "abc"},
	})
	msg := UpdateMessage("nginx:latest", "old", "new", "")
	if err := (webhookProvider{}).Send(context.Background(), string(cfg), msg); err != nil {
		t.Fatalf("webhook json send: %v", err)
	}
	req, body := get()
	if got := req.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Errorf("json format content-type = %q", got)
	}
	if got := req.Header.Get("X-Custom"); got != "abc" {
		t.Errorf("custom header missing, got %q", got)
	}
	var p webhookPayload
	if err := json.Unmarshal(body, &p); err != nil {
		t.Fatalf("unmarshal payload: %v (body=%s)", err, body)
	}
	if p.Title != msg.Title || p.Message != msg.Text || p.Markdown != msg.Markdown || p.Time == "" {
		t.Errorf("payload mismatch: %+v", p)
	}
}

func TestWebhookProviderText(t *testing.T) {
	url, get, cleanup := captureRequest(t)
	defer cleanup()
	cfg, _ := json.Marshal(map[string]string{"url": url, "format": "text"})
	msg := NewTagMessage("nginx:latest", "8.4.5", "9.0.0")
	if err := (webhookProvider{}).Send(context.Background(), string(cfg), msg); err != nil {
		t.Fatalf("webhook text send: %v", err)
	}
	req, body := get()
	if got := req.Header.Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("text format content-type = %q", got)
	}
	if string(body) != msg.Text {
		t.Errorf("text body mismatch:\n got %q\nwant %q", body, msg.Text)
	}
}

func TestWebhookValidate(t *testing.T) {
	if err := (webhookProvider{}).Validate(`{}`); err == nil {
		t.Error("webhook without url should fail validation")
	}
	if err := (webhookProvider{}).Validate(`{"url":"https://x","format":"yaml"}`); err == nil {
		t.Error("webhook invalid format should fail validation")
	}
	if err := (webhookProvider{}).Validate(`{"url":"https://x"}`); err != nil {
		t.Errorf("webhook valid config rejected: %v", err)
	}
}

func TestWecomProvider(t *testing.T) {
	url, get, cleanup := captureRequest(t)
	defer cleanup()
	cfg, _ := json.Marshal(map[string]string{"webhook": url})
	msg := TestMessage()
	if err := (wecomProvider{}).Send(context.Background(), string(cfg), msg); err != nil {
		t.Fatalf("wecom send: %v", err)
	}
	_, body := get()
	var m wecomMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal wecom: %v (body=%s)", err, body)
	}
	if m.MsgType != "markdown" || m.Markdown.Content != msg.Markdown {
		t.Errorf("wecom payload mismatch: %+v", m)
	}
}

func TestWecomValidate(t *testing.T) {
	if err := (wecomProvider{}).Validate(`{}`); err == nil {
		t.Error("wecom without webhook should fail validation")
	}
}

func TestFeishuProvider(t *testing.T) {
	url, get, cleanup := captureRequest(t)
	defer cleanup()
	cfg, _ := json.Marshal(map[string]string{"webhook": url})
	msg := TestMessage()
	if err := (feishuProvider{}).Send(context.Background(), string(cfg), msg); err != nil {
		t.Fatalf("feishu send: %v", err)
	}
	_, body := get()
	var c feishuCard
	if err := json.Unmarshal(body, &c); err != nil {
		t.Fatalf("unmarshal feishu: %v (body=%s)", err, body)
	}
	if c.MsgType != "interactive" || c.Card.Header.Title.Content != msg.Title {
		t.Errorf("feishu card header mismatch: %+v", c)
	}
	if len(c.Card.Elements) != 1 || c.Card.Elements[0].Text.Content != msg.Text {
		t.Errorf("feishu card element mismatch: %+v", c.Card.Elements)
	}
}

func TestFeishuValidate(t *testing.T) {
	if err := (feishuProvider{}).Validate(`{}`); err == nil {
		t.Error("feishu without webhook should fail validation")
	}
}

func TestTelegramProvider(t *testing.T) {
	url, get, cleanup := captureRequest(t)
	defer cleanup()
	cfg, _ := json.Marshal(map[string]string{"bot_token": "123:ABC", "chat_id": "-1001234", "api_base": url})
	msg := UpdateMessage("nginx:latest", "old", "new", "v1.4.1")
	if err := (telegramProvider{}).Send(context.Background(), string(cfg), msg); err != nil {
		t.Fatalf("telegram send: %v", err)
	}
	req, body := get()
	if !strings.Contains(req.URL.Path, "/bot123:ABC/sendMessage") {
		t.Errorf("telegram url path = %q", req.URL.Path)
	}
	var m telegramMessage
	if err := json.Unmarshal(body, &m); err != nil {
		t.Fatalf("unmarshal telegram: %v (body=%s)", err, body)
	}
	if m.ParseMode != "HTML" || m.ChatID != "-1001234" || m.Text != mdToHTML(msg.Markdown) {
		t.Errorf("telegram payload mismatch: %+v", m)
	}
}

func TestTelegramValidate(t *testing.T) {
	if err := (telegramProvider{}).Validate(`{}`); err == nil {
		t.Error("telegram empty config should fail validation")
	}
	if err := (telegramProvider{}).Validate(`{"bot_token":"x"}`); err == nil {
		t.Error("telegram without chat_id should fail validation")
	}
	if err := (telegramProvider{}).Validate(`{"bot_token":"x","chat_id":"y"}`); err != nil {
		t.Errorf("telegram valid config rejected: %v", err)
	}
}

func TestMDToHTML(t *testing.T) {
	in := "### ✅ 连通性测试\n\n**镜像**: `<nginx:latest>` & `other`\n"
	want := "<b>✅ 连通性测试</b>\n\n<b>镜像</b>: <code>&lt;nginx:latest&gt;</code> &amp; <code>other</code>"
	if got := mdToHTML(in); got != want {
		t.Errorf("mdToHTML mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestMessageBuilders(t *testing.T) {
	u := UpdateMessage("nginx:latest", "old", "new", "v1.4.1")
	if !strings.Contains(u.Markdown, "**最新Tag**: `v1.4.1`") || !strings.Contains(u.Text, "最新Tag: v1.4.1") {
		t.Errorf("update message missing tag line: %+v", u)
	}
	nt := NewTagMessage("postgres:15", "15", "16")
	if !strings.Contains(nt.Markdown, "**可选新版本**: `16`") || !strings.Contains(nt.Text, "可选新版本: 16") {
		t.Errorf("newtag message missing tag: %+v", nt)
	}
	if tm := TestMessage(); tm.Title == "" || tm.Markdown == "" || tm.Text == "" {
		t.Errorf("test message incomplete: %+v", tm)
	}
}
