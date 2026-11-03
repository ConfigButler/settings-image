// SPDX-License-Identifier: Apache-2.0

package settingsimage_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// headerLog records the Authorization header of every request a test server receives.
type headerLog struct {
	mu   sync.Mutex
	seen []string
}

func (l *headerLog) record(r *http.Request) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.seen = append(l.seen, r.Method+" "+r.URL.Path+" "+r.Header.Get("Authorization"))
}

func (l *headerLog) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]string{}, l.seen...)
}

// newRepository is NewRepository for a test server (plain HTTP, host:port/settings), as its HTTP client.
func newRepository(t *testing.T, srv *httptest.Server, opts settingsimage.RepositoryOptions) testClient {
	t.Helper()
	opts.PlainHTTP = true
	repo, err := settingsimage.NewRepository(strings.TrimPrefix(srv.URL, "http://")+"/settings", opts)
	if err != nil {
		t.Fatal(err)
	}
	return testClient{repo.Client, strings.TrimPrefix(srv.URL, "http://")}
}

type testClient struct {
	client interface {
		Do(*http.Request) (*http.Response, error)
	}
	host string
}

func (r testClient) get(t *testing.T, url string) int {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := r.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

// TestRegistryTokenStaysOnTheRegistry is cli.md §4: a registry token goes, as Bearer, to every
// request to the registry, without waiting for a challenge, and never follows a redirect to
// storage on another host.
func TestRegistryTokenStaysOnTheRegistry(t *testing.T) {
	var storageLog, registryLog headerLog
	storage := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		storageLog.record(r)
		_, _ = w.Write([]byte("blob"))
	}))
	defer storage.Close()
	registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		registryLog.record(r)
		if strings.Contains(r.URL.Path, "/blobs/") {
			http.Redirect(w, r, storage.URL+"/bucket/blob", http.StatusTemporaryRedirect)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer registry.Close()

	repo := newRepository(t, registry, settingsimage.RepositoryOptions{RegistryToken: "s3cr3t"})
	assertEqual(t, "GET /v2/", repo.get(t, registry.URL+"/v2/"), http.StatusOK)
	assertEqual(t, "GET blob", repo.get(t, registry.URL+"/v2/settings/blobs/sha256:00"), http.StatusOK)

	want := []string{"GET /v2/ Bearer s3cr3t", "GET /v2/settings/blobs/sha256:00 Bearer s3cr3t"}
	assertEqual(t, "registry saw", strings.Join(registryLog.all(), "\n"), strings.Join(want, "\n"))
	assertEqual(t, "storage saw", strings.Join(storageLog.all(), "\n"), "GET /bucket/blob ")
}

func TestRegistryTokenAndPasswordAreOneTooMany(t *testing.T) {
	_, err := settingsimage.NewRepository("registry.example.com/settings", settingsimage.RepositoryOptions{
		Username: "ada", Password: "pw", RegistryToken: "tok",
	})
	if !errors.Is(err, settingsimage.ErrTwoCredentials) {
		t.Fatalf("err = %v, want ErrTwoCredentials", err)
	}
}

// TestRefusedCredentialIsAnAuthError: a 401 or 403 becomes an AuthError naming the registry,
// and the credential appears in no error.
func TestRefusedCredentialIsAnAuthError(t *testing.T) {
	for name, status := range map[string]int{"401": http.StatusUnauthorized, "403": http.StatusForbidden} {
		t.Run(name, func(t *testing.T) {
			registry := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"errors":[{"code":"DENIED","message":"token not accepted"}]}`))
			}))
			defer registry.Close()
			repo, err := settingsimage.NewRepository(strings.TrimPrefix(registry.URL, "http://")+"/settings",
				settingsimage.RepositoryOptions{PlainHTTP: true, RegistryToken: "s3cr3t"})
			if err != nil {
				t.Fatal(err)
			}
			img, err := settingsimage.Build(t.Context(), settingsimage.Tree{"raw/a.txt": []byte("a\n")},
				settingsimage.BuildOptions{})
			if err != nil {
				t.Fatal(err)
			}
			_, err = img.Push(t.Context(), repo, []string{"main"})
			var denied *settingsimage.AuthError
			if !errors.As(err, &denied) {
				t.Fatalf("err = %v, want an AuthError", err)
			}
			assertEqual(t, "registry", denied.Registry, strings.TrimPrefix(registry.URL, "http://"))
			if strings.Contains(err.Error(), "s3cr3t") {
				t.Errorf("the error carries the token: %v", err)
			}
		})
	}
}
