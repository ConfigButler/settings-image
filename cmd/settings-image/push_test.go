// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// denyingRegistry refuses every request with 401, and records the Authorization headers it saw.
type denyingRegistry struct {
	*httptest.Server

	mu   sync.Mutex
	auth map[string]bool
}

func newDenyingRegistry(t *testing.T) *denyingRegistry {
	t.Helper()
	d := &denyingRegistry{auth: map[string]bool{}}
	d.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d.mu.Lock()
		d.auth[r.Header.Get("Authorization")] = true
		d.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"errors":[{"code":"UNAUTHORIZED","message":"bad token"}]}`))
	}))
	t.Cleanup(d.Close)
	return d
}

func (d *denyingRegistry) repository() string {
	return strings.TrimPrefix(d.URL, "http://") + "/settings"
}

func (d *denyingRegistry) saw(header string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.auth[header]
}

func cliStdin(t *testing.T, stdin string, args ...string) (int, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), args, strings.NewReader(stdin), &stdout, &stderr)
	return code, stderr.String()
}

// TestPushRegistryToken is cli.md §4 and §6: a registry token from stdin or the environment is
// sent as Bearer; a refused credential exits 4, naming the registry and never the token.
func TestPushRegistryToken(t *testing.T) {
	r := newRepo(t)
	t.Run("stdin", func(t *testing.T) {
		reg := newDenyingRegistry(t)
		code, stderr := cliStdin(t, "s3cr3t\n", "push", r.shop(), "--repository", reg.repository(),
			"--plain-http", "--registry-token-stdin")
		assertDenied(t, reg, code, stderr)
	})
	t.Run("environment", func(t *testing.T) {
		reg := newDenyingRegistry(t)
		t.Setenv("SETTINGS_IMAGE_REGISTRY_TOKEN", "s3cr3t")
		code, stderr := cliStdin(t, "", "push", r.shop(), "--repository", reg.repository(), "--plain-http")
		assertDenied(t, reg, code, stderr)
	})
	t.Run("token and password in the environment", func(t *testing.T) {
		t.Setenv("SETTINGS_IMAGE_REGISTRY_TOKEN", "s3cr3t")
		t.Setenv("SETTINGS_IMAGE_PASSWORD", "pw")
		code, stderr := cliStdin(t, "", "push", r.shop(), "--repository", "127.0.0.1:1/settings", "--plain-http")
		if code != exitUsage {
			t.Errorf("exit %d, want %d; stderr %q", code, exitUsage, stderr)
		}
	})
	t.Run("empty stdin", func(t *testing.T) {
		code, stderr := cliStdin(t, "\n", "push", r.shop(), "--repository", "127.0.0.1:1/settings",
			"--plain-http", "--registry-token-stdin")
		if code != exitUsage {
			t.Errorf("exit %d, want %d; stderr %q", code, exitUsage, stderr)
		}
	})
}

func assertDenied(t *testing.T, reg *denyingRegistry, code int, stderr string) {
	t.Helper()
	if code != exitDenied {
		t.Errorf("exit %d, want %d; stderr %q", code, exitDenied, stderr)
	}
	if !reg.saw("Bearer s3cr3t") {
		t.Error("the registry never saw the token as Bearer")
	}
	if !strings.Contains(stderr, strings.TrimPrefix(reg.URL, "http://")) || !strings.Contains(stderr, "refused") {
		t.Errorf("stderr does not name the registry and the refusal: %q", stderr)
	}
	if strings.Contains(stderr, "s3cr3t") {
		t.Errorf("stderr carries the token: %q", stderr)
	}
}
