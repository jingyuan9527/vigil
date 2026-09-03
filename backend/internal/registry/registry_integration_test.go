package registry

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestManifestDigestAuthFlow 通过 httptest 模拟 Docker Hub 注册表的
// 401 -> Bearer token 交换 -> 带 token 重试并返回 Docker-Content-Digest 的完整流程。
func TestManifestDigestAuthFlow(t *testing.T) {
	var manifestCalled, tokenCalled int
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/library/nginx/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		manifestCalled++
		if r.Header.Get("Authorization") != "Bearer good-token" {
			realm := "http://" + r.Host + "/token"
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+realm+`",service="registry.docker.io",scope="repository:library/nginx:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Docker-Content-Digest", "sha256:abc123")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalled++
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"good-token"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	c := &Client{http: srv.Client(), insecure: true, userAgent: "test"}
	dig, err := c.ManifestDigest(context.Background(), ImageRef{Registry: host, Repo: "library/nginx", Tag: "latest"})
	if err != nil {
		t.Fatalf("ManifestDigest: %v", err)
	}
	if dig != "abc123" {
		t.Fatalf("digest = %q, want abc123", dig)
	}
	if manifestCalled != 2 || tokenCalled != 1 {
		t.Fatalf("manifestCalled=%d tokenCalled=%d (want 2/1)", manifestCalled, tokenCalled)
	}
}

// TestListTagsAuthFlow 验证 tags/list 接口的鉴权与解析。
func TestListTagsAuthFlow(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/library/redis/tags/list", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			realm := "http://" + r.Host + "/token"
			w.Header().Set("WWW-Authenticate",
				`Bearer realm="`+realm+`",service="registry.docker.io",scope="repository:library/redis:pull"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"name":"library/redis","tags":["7","7-alpine","latest"]}`))
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"token":"t"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	c := &Client{http: srv.Client(), insecure: true, userAgent: "test"}
	tags, err := c.ListTags(context.Background(), ImageRef{Registry: host, Repo: "library/redis", Tag: "latest"})
	if err != nil {
		t.Fatalf("ListTags: %v", err)
	}
	if len(tags) != 3 || tags[0] != "7" {
		t.Fatalf("tags = %v, want [7 7-alpine latest]", tags)
	}
}

// TestManifestDigestNoAuth 验证无需鉴权的注册表（已带正确 digest 直接返回）。
func TestManifestDigestNoAuth(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/v2/library/busybox/manifests/latest", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Docker-Content-Digest", "sha256:feed")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	host := strings.TrimPrefix(srv.URL, "http://")
	c := &Client{http: srv.Client(), insecure: true, userAgent: "test"}
	dig, err := c.ManifestDigest(context.Background(), ImageRef{Registry: host, Repo: "library/busybox", Tag: "latest"})
	if err != nil {
		t.Fatalf("ManifestDigest: %v", err)
	}
	if dig != "feed" {
		t.Fatalf("digest = %q, want feed", dig)
	}
}
