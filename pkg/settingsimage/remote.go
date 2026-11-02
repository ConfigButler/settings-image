// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"errors"
	"fmt"
	"net/http"

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
	// Username and Password are a static credential. When Password is empty, the Docker config
	// and its credential helpers are used, as `docker login` left them, and failing those the
	// registry is accessed anonymously.
	Username string
	Password string
}

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
// does not fail a push (format-v1.md §7).
func NewRepository(reference string, opts RepositoryOptions) (*remote.Repository, error) {
	repo, err := remote.NewRepository(reference)
	if err != nil {
		return nil, fmt.Errorf("repository %q: %w", reference, err)
	}
	repo.PlainHTTP = opts.PlainHTTP
	repo.SkipReferrersGC = true

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
