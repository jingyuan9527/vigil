package docker

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingAndListImageRefs(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/_ping", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.HandleFunc("/images/json", func(w http.ResponseWriter, r *http.Request) {
		imgs := []imageSummary{
			{
				Id:          "sha256:1",
				RepoTags:    []string{"nginx:latest", "nginx:1.27"},
				RepoDigests: []string{"nginx@sha256:deadbeef"},
			},
			{
				Id:          "sha256:2",
				RepoTags:    []string{"<none>:<none>"},
				RepoDigests: []string{},
			},
		}
		_ = json.NewEncoder(w).Encode(imgs)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClientForTest(srv.URL, srv.Client())

	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	refs, err := c.ListImageRefs(context.Background())
	if err != nil {
		t.Fatalf("ListImageRefs: %v", err)
	}
	if refs["nginx:latest"] != "deadbeef" {
		t.Fatalf("nginx:latest digest = %q, want deadbeef (sha256: prefix must be stripped)", refs["nginx:latest"])
	}
	if _, ok := refs["nginx:1.27"]; !ok {
		t.Fatalf("nginx:1.27 missing from result: %v", refs)
	}
	if _, ok := refs["<none>:<none>"]; ok {
		t.Fatalf("should skip <none>:<none>, got: %v", refs)
	}
}
