package notification

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
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
