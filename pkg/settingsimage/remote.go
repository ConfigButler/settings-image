// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"

	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/credentials"
	"oras.land/oras-go/v2/registry/remote/errcode"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// RepositoryOptions configure [NewRepository].
type RepositoryOptions struct {
	// PlainHTTP talks HTTP instead of HTTPS, for a local registry.
	PlainHTTP bool
	// Username and Password are a static credential.
	Username string
	Password string
	// RegistryToken is a bearer token, sent unchanged as "Authorization: Bearer" on every request
	// to the registry's own origin, from the first request on, whatever challenge the registry
	// would answer with, and never to any other origin: a blob request redirected to storage
	// goes without it. A zot that accepts a GitHub Actions OIDC token takes it this way.
	//
	// It cannot be combined with Password. With neither, the Docker config and its credential
	// helpers are used, as `docker login` left them, and failing those the registry is accessed
	// anonymously.
	RegistryToken string
}

// ErrTwoCredentials is returned by [NewRepository] when both a password and a registry token are
// given: a run uses one credential, never a guess between two.
var ErrTwoCredentials = errors.New("a password and a registry token were both given; use one")

// AuthError is a registry refusing the credential (HTTP 401 or 403).
type AuthError struct {
	Registry string
	Err      error
}

func (e *AuthError) Error() string {
	return fmt.Sprintf("registry %s refused the credential: %v", e.Registry, e.Err)
}

func (e *AuthError) Unwrap() error { return e.Err }

// NewRepository is a remote repository (host/path, without a tag) set up as a writer needs it:
// oras-go's retry client, credentials, and SkipReferrersGC, so a registry that refuses DELETE
// does not fail a push (format-v2.md §9).
func NewRepository(reference string, opts RepositoryOptions) (*remote.Repository, error) {
	if opts.Password != "" && opts.RegistryToken != "" {
		return nil, ErrTwoCredentials
	}
	repo, err := remote.NewRepository(reference)
	if err != nil {
		return nil, fmt.Errorf("repository %q: %w", reference, err)
	}
	repo.PlainHTTP = opts.PlainHTTP
	repo.SkipReferrersGC = true

	if opts.RegistryToken != "" {
		scheme := "https"
		if opts.PlainHTTP {
			scheme = "http"
		}
		repo.Client = &http.Client{Transport: &bearerTransport{
			origin: origin(scheme, repo.Reference.Host()),
			token:  opts.RegistryToken,
			base:   retry.NewTransport(nil),
		}}
		return repo, nil
	}

	client := &auth.Client{Client: retry.DefaultClient, Cache: auth.NewCache()}
	client.SetUserAgent("settings-image")
	registry := repo.Reference.Registry
	switch {
	case opts.Password != "":
		client.Credential = auth.StaticCredential(registry, auth.Credential{
			Username: opts.Username,
			Password: opts.Password,
		})
	default:
		if store, err := credentials.NewStoreFromDocker(credentials.StoreOptions{}); err == nil {
			client.Credential = credentials.Credential(store)
		}
	}
	repo.Client = client
	return repo, nil
}

// bearerTransport sends a registry token as "Authorization: Bearer" to one origin only. It sees
// every hop of a redirect, and adds the header to its own copy of the request, so the http.Client
// cannot carry the header on to another host.
type bearerTransport struct {
	origin string // scheme://host:port
	token  string
	base   http.RoundTripper
}

func (t *bearerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req = req.Clone(req.Context())
	req.Header.Set("User-Agent", "settings-image")
	if origin(req.URL.Scheme, req.URL.Host) == t.origin {
		req.Header.Set("Authorization", "Bearer "+t.token)
	} else {
		req.Header.Del("Authorization")
	}
	return t.base.RoundTrip(req)
}

// origin is scheme://host:port, with the scheme's default port made explicit, so that
// "registry.example.com" and "registry.example.com:443" compare equal over https.
func origin(scheme, host string) string {
	scheme = strings.ToLower(scheme)
	host = strings.ToLower(host)
	if _, _, err := net.SplitHostPort(host); err != nil {
		port := "443"
		if scheme == "http" {
			port = "80"
		}
		host = net.JoinHostPort(strings.Trim(host, "[]"), port)
	}
	return scheme + "://" + host
}

// classify turns a registry's 401 or 403 into an [*AuthError]. It never includes a credential:
// oras-go's error responses carry the URL and the registry's message only.
func classify(err error) error {
	var resp *errcode.ErrorResponse
	if errors.As(err, &resp) &&
		(resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden) {
		host := ""
		if resp.URL != nil {
			host = resp.URL.Host
		}
		return &AuthError{Registry: host, Err: err}
	}
	return err
}
