package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// Client 通过 Docker Engine API（HTTP）与守护进程通信。
// 支持 unix socket（Linux 容器场景，挂载 /var/run/docker.sock）与 tcp。
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient 根据 DOCKER_HOST 构造客户端。
// 空值或 unix:// 前缀使用默认 /var/run/docker.sock。
func NewClient(dockerHost string) (*Client, error) {
	if dockerHost == "" {
		dockerHost = "unix:///var/run/docker.sock"
	}
	switch {
	case strings.HasPrefix(dockerHost, "unix://"):
		sock := strings.TrimPrefix(dockerHost, "unix://")
		return &Client{
			baseURL: "http://unix",
			http: &http.Client{
				Transport: &http.Transport{
					DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
						d := net.Dialer{}
						return d.DialContext(ctx, "unix", sock)
					},
				},
			},
		}, nil
	case strings.HasPrefix(dockerHost, "tcp://"):
		addr := strings.TrimPrefix(dockerHost, "tcp://")
		return &Client{
			baseURL: "http://" + addr,
			http:    &http.Client{Timeout: 15 * time.Second},
		}, nil
	default:
		return nil, fmt.Errorf("unsupported DOCKER_HOST: %s (supported: unix://, tcp://)", dockerHost)
	}
}

type imageSummary struct {
	Id          string   `json:"Id"`
	RepoTags    []string `json:"RepoTags"`
	RepoDigests []string `json:"RepoDigests"`
}

// NewClientForTest 使用给定的 baseURL 与 http.Client 构造客户端，便于集成测试。
// 其 baseURL 直接指向测试用 httptest 服务，绕过 unix/tcp 解析逻辑。
func NewClientForTest(baseURL string, hc *http.Client) *Client {
	return &Client{baseURL: baseURL, http: hc}
}

// Ping 检查 Docker 守护进程是否可达。
func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping status %d", resp.StatusCode)
	}
	return nil
}

// ListImageRefs 返回 map[reference]digest，reference 形如 nginx:latest。
// 摘要取自 RepoDigests（已拉取镜像的 registry 内容摘要）。
func (c *Client) ListImageRefs(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/images/json", nil)
	if err != nil {
		return nil, err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("docker images.json status %d", resp.StatusCode)
	}
	var imgs []imageSummary
	if err := json.NewDecoder(resp.Body).Decode(&imgs); err != nil {
		return nil, err
	}
	out := map[string]string{}
	for _, img := range imgs {
		digest := ""
		for _, d := range img.RepoDigests {
			if i := strings.Index(d, "@"); i >= 0 {
				// RepoDigests 形如 "nginx@sha256:abc..."，统一剥离 sha256: 前缀，
				// 保证与 registry 返回的裸摘要可直接比较。
				digest = strings.TrimPrefix(d[i+1:], "sha256:")
				break
			}
		}
		for _, tag := range img.RepoTags {
			if tag == "<none>:<none>" {
				continue
			}
			out[tag] = digest
		}
	}
	return out, nil
}
