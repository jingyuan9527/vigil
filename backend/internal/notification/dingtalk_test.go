package notification

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
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

// TestNotifyUpdateLatestTagLine 校验镜像更新提醒：latestTag 非空时附上
// 「最新Tag」行，为空时不输出该行（老格式保持不变）。
func TestNotifyUpdateLatestTagLine(t *testing.T) {
	url, getText, cleanup := captureDingTalk(t)
	defer cleanup()

	if err := NotifyUpdate(url, "", "ghcr.io/jingyuan9527/stellar:latest",
		"4611266078ea", "45f6f511ca", "v1.4.1"); err != nil {
		t.Fatalf("NotifyUpdate with tag: %v", err)
	}
	got := getText()
	for _, want := range []string{"**镜像**: ghcr.io/jingyuan9527/stellar:latest",
		"**最新Tag**: `v1.4.1`", "**旧摘要**: `4611266078ea`", "**新摘要**: `45f6f511ca`"} {
		if strings.Count(got, want) == 0 {
			t.Errorf("content missing %q:\n%s", want, got)
		}
	}

	if err := NotifyUpdate(url, "", "nginx:latest", "old", "new", ""); err != nil {
		t.Fatalf("NotifyUpdate without tag: %v", err)
	}
	got = getText()
	if strings.Contains(got, "最新Tag") {
		t.Errorf("content should not contain 最新Tag when latestTag empty:\n%s", got)
	}
}
