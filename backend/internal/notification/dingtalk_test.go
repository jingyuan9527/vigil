package notification

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestSignURLEmptySecret(t *testing.T) {
	raw := "https://oapi.dingtalk.com/robot/send?access_token=x"
	if got := signURL(raw, ""); got != raw {
		t.Fatalf("empty secret should return url unchanged, got %q", got)
	}
}

// TestSignURLMatchesDingTalk 校验加签结果与钉钉官方算法一致：
// sign = URLEncode(base64(HMAC-SHA256("<timestamp毫秒>\n<secret>", secret)))。
func TestSignURLMatchesDingTalk(t *testing.T) {
	raw := "https://oapi.dingtalk.com/robot/send?access_token=x"
	secret := "my-secret"
	signed := signURL(raw, secret)

	u, err := url.Parse(signed)
	if err != nil {
		t.Fatalf("parse signed url: %v", err)
	}
	ts := u.Query().Get("timestamp")
	sign := u.Query().Get("sign")
	if ts == "" || sign == "" {
		t.Fatalf("missing timestamp/sign params in %q", signed)
	}
	ms, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		t.Fatalf("timestamp not int: %v", err)
	}
	if time.Now().UnixMilli()-ms > 60_000 {
		t.Fatalf("timestamp deviates too far from now")
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(ts + "\n" + secret))
	expect := base64.StdEncoding.EncodeToString(mac.Sum(nil))
	// Query().Get 已按 form 语义把 %2B 还原为 '+'，不能再 QueryUnescape——
	// 二次解码会把 '+' 当空格吃掉（历史实现正栽在这里，且仅当签名恰含 '+' 时失败）。
	if sign != expect {
		t.Fatalf("sign mismatch: got %q want %q", sign, expect)
	}
}

// captureDingTalk 起一个假 webhook 服务器，返回请求体中的 text 内容与清理函数。
func captureDingTalk(t *testing.T) (url string, getText func() string, cleanup func()) {
	t.Helper()
	var mu sync.Mutex
	var text string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("timestamp") == "" {
			t.Errorf("signed request expected, missing timestamp query param")
		}
		var msg struct {
			Markdown struct {
				Text string `json:"text"`
			} `json:"markdown"`
		}
		if err := json.NewDecoder(r.Body).Decode(&msg); err != nil {
			t.Errorf("decode request body: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		text = msg.Markdown.Text
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	return srv.URL,
		func() string {
			mu.Lock()
			defer mu.Unlock()
			return text
		},
		func() { srv.Close() }
}

// TestDingTalkProviderUpdateMessage 校验钉钉渠道在真实发送链路上使用更新消息的 Markdown 内容：
// latestTag 非空时附「最新Tag」行，为空时不输出该行（老格式保持一致）。
func TestDingTalkProviderUpdateMessage(t *testing.T) {
	for _, tc := range []struct {
		latestTag   string
		wantContain string
		wantAbsent  string
	}{
		{"v1.4.1", "**最新Tag**: `v1.4.1`", ""},
		{"", "", "最新Tag"},
	} {
		url, getText, cleanup := captureDingTalk(t)
		cfg, _ := json.Marshal(map[string]string{"webhook": url, "secret": "secret"})
		msg := UpdateMessage("ghcr.io/jingyuan9527/stellar:latest", "4611266078ea", "45f6f511ca", tc.latestTag)
		if err := (dingtalkProvider{}).Send(context.Background(), string(cfg), msg); err != nil {
			cleanup()
			t.Fatalf("dingtalk send with tag %q: %v", tc.latestTag, err)
		}
		got := getText()
		cleanup()
		for _, want := range []string{"**镜像**: ghcr.io/jingyuan9527/stellar:latest",
			"**旧摘要**: `4611266078ea`", "**新摘要**: `45f6f511ca`"} {
			if !strings.Contains(got, want) {
				t.Errorf("content missing %q (latestTag=%q):\n%s", want, tc.latestTag, got)
			}
		}
		if tc.wantContain != "" && !strings.Contains(got, tc.wantContain) {
			t.Errorf("content missing %q (latestTag=%q):\n%s", tc.wantContain, tc.latestTag, got)
		}
		if tc.wantAbsent != "" && strings.Contains(got, tc.wantAbsent) {
			t.Errorf("content should not contain %q when latestTag=%q:\n%s", tc.wantAbsent, tc.latestTag, got)
		}
	}
}

// TestDingTalkValidate 校验钉钉渠道缺 webhook 时保存/发送均被拦截。
func TestDingTalkValidate(t *testing.T) {
	if err := (dingtalkProvider{}).Validate(`{}`); err == nil {
		t.Error("dingtalk empty config should fail validation")
	}
	if err := (dingtalkProvider{}).Validate(`{"webhook":"https://x"}`); err != nil {
		t.Errorf("dingtalk valid config rejected: %v", err)
	}
}

// TestSendUnknownKind 确保未注册的 kind 在发送与校验时都报错（配置错误尽早暴露）。
func TestSendUnknownKind(t *testing.T) {
	if err := Send(context.Background(), "no-such-kind", "{}", TestMessage()); err == nil {
		t.Error("Send should error on unknown kind")
	}
	if err := Validate("no-such-kind", "{}"); err == nil {
		t.Error("Validate should error on unknown kind")
	}
}

// TestSendNilContext 空 context 也应正常工作（发送路径兜底为 Background）。
func TestSendNilContext(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	cfg, _ := json.Marshal(map[string]string{"webhook": srv.URL})
	msg := TestMessage()
	if err := Send(nil, "dingtalk", string(cfg), msg); err != nil {
		t.Fatalf("dingtalk send with nil ctx: %v", err)
	}
}
