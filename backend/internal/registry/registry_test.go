package registry

import "testing"

func TestParseRef(t *testing.T) {
	cases := []struct {
		in   string
		reg  string
		repo string
		tag  string
	}{
		{"nginx:latest", "registry-1.docker.io", "library/nginx", "latest"},
		{"nginx", "registry-1.docker.io", "library/nginx", "latest"},
		{"redis:7", "registry-1.docker.io", "library/redis", "7"},
		{"user/foo:1.0", "registry-1.docker.io", "user/foo", "1.0"},
		{"ghcr.io/foo/bar:1.0", "ghcr.io", "foo/bar", "1.0"},
		{"localhost:5000/x", "localhost:5000", "x", "latest"},
		{"registry.example.com:5000/team/app:v2", "registry.example.com:5000", "team/app", "v2"},
		{"docker.io/library/nginx:alpine", "registry-1.docker.io", "library/nginx", "alpine"},
		{"quay.io/coreos/etcd:v3.5", "quay.io", "coreos/etcd", "v3.5"},
	}
	for _, c := range cases {
		r := ParseRef(c.in)
		if r.Registry != c.reg || r.Repo != c.repo || r.Tag != c.tag {
			t.Errorf("ParseRef(%q) = {%q,%q,%q}, want {%q,%q,%q}",
				c.in, r.Registry, r.Repo, r.Tag, c.reg, c.repo, c.tag)
		}
	}
}

func TestParseAuthParams(t *testing.T) {
	header := `Bearer realm="https://auth.docker.io/token",service="registry.docker.io",scope="repository:library/nginx:pull"`
	p := parseAuthParams(header[len("Bearer "):])
	if p["realm"] != "https://auth.docker.io/token" {
		t.Errorf("realm = %q", p["realm"])
	}
	if p["service"] != "registry.docker.io" {
		t.Errorf("service = %q", p["service"])
	}
	if p["scope"] != "repository:library/nginx:pull" {
		t.Errorf("scope = %q", p["scope"])
	}
}
