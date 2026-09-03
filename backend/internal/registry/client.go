package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// ImageRef 解析后的镜像引用。
type ImageRef struct {
	Registry string
	Repo     string
	Tag      string
}

// ParseRef 解析镜像引用为 Registry/Repo/Tag。
// 例：nginx:latest -> registry-1.docker.io / library/nginx / latest
//
//	ghcr.io/foo/bar:1.0 -> ghcr.io / foo/bar / 1.0
//	localhost:5000/x -> localhost:5000 / x / latest
func ParseRef(ref string) ImageRef {
	ref = strings.TrimSpace(ref)
	tag := "latest"
	name := ref
	if i := strings.LastIndex(ref, ":"); i > 0 {
		// 仅当冒号后不含 "/" 时才视为 tag（排除 localhost:5000 这类端口）
		if !strings.Contains(ref[i:], "/") {
			name = ref[:i]
			tag = ref[i+1:]
		}
	}
	registry := "registry-1.docker.io"
	repo := name
	if i := strings.Index(name, "/"); i > 0 {
		first := name[:i]
		if strings.ContainsAny(first, ".:") || first == "localhost" {
			registry = first
			repo = name[i+1:]
		}
	}
	if registry == "docker.io" {
		registry = "registry-1.docker.io"
	}
	if registry == "registry-1.docker.io" && !strings.Contains(repo, "/") {
		repo = "library/" + repo
	}
	return ImageRef{Registry: registry, Repo: repo, Tag: tag}
}

// Client 是与 OCI/Docker 注册表通信的客户端，支持 Bearer 鉴权。
type Client struct {
	http      *http.Client
	insecure  bool
	userAgent string
	mirror    string // 注册表镜像：非空时覆盖 ref.Registry 的主机（私有仓库/加速场景）
}

func NewClient(insecure bool) *Client {
	return NewClientWithMirror(insecure, "")
}

// NewClientWithMirror 构造客户端；mirror 非空时将所有 manifest/tag 请求发往该主机。
func NewClientWithMirror(insecure bool, mirror string) *Client {
	return NewClientWithMirrorAndHTTP(insecure, mirror, nil)
}

// NewClientWithMirrorAndHTTP 允许注入自定义 http.Client（测试或特殊网络场景，
// 例如显式关闭代理）。hc 为 nil 时使用默认 20s 超时的客户端（沿用环境代理配置）。
func NewClientWithMirrorAndHTTP(insecure bool, mirror string, hc *http.Client) *Client {
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &Client{
		http:      hc,
		insecure:  insecure,
		userAgent: "dockmon/1.0",
		mirror:    mirror,
	}
}

// host 返回实际请求的主机：配置了镜像则优先使用镜像主机。
func (c *Client) host(ref ImageRef) string {
	if c.mirror != "" {
		return c.mirror
	}
	return ref.Registry
}

func (c *Client) scheme() string {
	if c.insecure {
		return "http"
	}
	return "https"
}

const manifestAccept = "application/vnd.docker.distribution.manifest.v2+json," +
	"application/vnd.docker.distribution.manifest.list.v2+json," +
	"application/vnd.oci.image.index.v1+json," +
	"application/vnd.oci.image.manifest.v1+json"

// ManifestDigest 获取指定 tag 当前 manifest 的 Docker-Content-Digest。
func (c *Client) ManifestDigest(ctx context.Context, ref ImageRef) (string, error) {
	url := fmt.Sprintf("%s://%s/v2/%s/manifests/%s", c.scheme(), c.host(ref), ref.Repo, ref.Tag)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", c.userAgent)
	req.Header.Set("Accept", manifestAccept)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		token, terr := c.getToken(ctx, resp, ref)
		if terr != nil {
			resp.Body.Close()
			return "", terr
		}
		req2, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		req2.Header.Set("User-Agent", c.userAgent)
		req2.Header.Set("Accept", manifestAccept)
		req2.Header.Set("Authorization", "Bearer "+token)
		resp2, derr := c.http.Do(req2)
		resp.Body.Close()
		if derr != nil {
			return "", derr
		}
		resp = resp2
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("registry manifest %s status %d", ref.Repo, resp.StatusCode)
	}
	dig := resp.Header.Get("Docker-Content-Digest")
	return strings.TrimPrefix(dig, "sha256:"), nil
}

// ListTags 尽力获取仓库可用 tag 列表（部分注册表禁用该接口）。
func (c *Client) ListTags(ctx context.Context, ref ImageRef) ([]string, error) {
	url := fmt.Sprintf("%s://%s/v2/%s/tags/list", c.scheme(), c.host(ref), ref.Repo)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", c.userAgent)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusUnauthorized {
		token, terr := c.getToken(ctx, resp, ref)
		if terr != nil {
			resp.Body.Close()
			return nil, terr
		}
		req.Header.Set("Authorization", "Bearer "+token)
		resp2, derr := c.http.Do(req)
		resp.Body.Close()
		if derr != nil {
			return nil, derr
		}
		resp = resp2
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("registry tags/list %s status %d", ref.Repo, resp.StatusCode)
	}
	var t struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&t); err != nil {
		return nil, err
	}
	return t.Tags, nil
}

// getToken 根据 401 响应中的 WWW-Authenticate 头获取 Bearer token。
func (c *Client) getToken(ctx context.Context, resp *http.Response, ref ImageRef) (string, error) {
	auth := resp.Header.Get("WWW-Authenticate")
	realm, service, scope := "", "", fmt.Sprintf("repository:%s:pull", ref.Repo)

	if strings.HasPrefix(auth, "Bearer ") {
		params := parseAuthParams(auth[len("Bearer "):])
		realm = params["realm"]
		if params["service"] != "" {
			service = params["service"]
		}
		if params["scope"] != "" {
			scope = params["scope"]
		}
	}
	if realm == "" {
		// Docker Hub 默认鉴权端点
		realm = "https://auth.docker.io/token"
		service = "registry.docker.io"
	}

	u := fmt.Sprintf("%s?service=%s&scope=%s", realm, service, scope)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("User-Agent", c.userAgent)
	r, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer r.Body.Close()
	if r.StatusCode != http.StatusOK {
		return "", fmt.Errorf("token endpoint status %d", r.StatusCode)
	}
	var tr struct {
		Token       string `json:"token"`
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&tr); err != nil {
		return "", err
	}
	if tr.Token != "" {
		return tr.Token, nil
	}
	return tr.AccessToken, nil
}

func parseAuthParams(s string) map[string]string {
	out := map[string]string{}
	for _, part := range strings.Split(s, ",") {
		kv := strings.SplitN(part, "=", 2)
		if len(kv) == 2 {
			out[strings.TrimSpace(kv[0])] = strings.Trim(kv[1], `"`)
		}
	}
	return out
}
