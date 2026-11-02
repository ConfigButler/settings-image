// SPDX-License-Identifier: Apache-2.0

// Package gitinfo reads what a settings image's provenance and sha- tag need from Git, by
// running the git CLI (docs/cli.md §3). It links no Git library.
package gitinfo

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// ErrRefused is wrapped by every error that a retry cannot fix: not a work tree, a dirty folder,
// a shallow clone, a folder without commits.
var ErrRefused = errors.New("refused")

// Options are the CLI's escapes from the rules.
type Options struct {
	AllowShallow bool
}

// Info is the commit that last changed a folder, and where it came from.
type Info struct {
	Commit      string
	AuthorName  string
	AuthorEmail string
	Subject     string
	CommittedAt string // RFC 3339, UTC
	Ref         string
	Path        string // the folder relative to the repository root, slash-separated; "." for the root
	// Shallow is true when the clone is shallow and AllowShallow let it through: Commit is then
	// HEAD, not the commit that last changed the folder.
	Shallow bool
}

// CheckWorkTree refuses a folder outside a Git work tree.
func CheckWorkTree(ctx context.Context, folder string) error {
	if _, err := git(ctx, folder, "rev-parse", "--show-toplevel"); err != nil {
		return fmt.Errorf("%w: %s is not inside a Git work tree (use --no-provenance)", ErrRefused, folder)
	}
	return nil
}

// Dirty reports whether the folder has uncommitted changes, untracked files included.
func Dirty(ctx context.Context, folder string) (bool, error) {
	out, err := git(ctx, folder, "status", "--porcelain", "--", ".")
	if err != nil {
		return false, err
	}
	return out != "", nil
}

// Read returns the commit that last changed folder. The folder must be in a Git work tree.
func Read(ctx context.Context, folder string, opts Options) (*Info, error) {
	if err := CheckWorkTree(ctx, folder); err != nil {
		return nil, err
	}
	info := &Info{}
	shallow, err := git(ctx, folder, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, err
	}
	logArgs := []string{"log", "-1", "--format=%H%x00%an%x00%ae%x00%s%x00%cI"}
	if shallow == "true" {
		if !opts.AllowShallow {
			return nil, fmt.Errorf("%w: a shallow clone cannot tell which commit last changed %s; "+
				"fetch with depth 0, or use --allow-shallow", ErrRefused, folder)
		}
		info.Shallow = true
		logArgs = append(logArgs, "HEAD")
	} else {
		logArgs = append(logArgs, "--", ".")
	}

	out, err := git(ctx, folder, logArgs...)
	if err != nil {
		return nil, err
	}
	const fields = 5
	parts := strings.Split(out, "\x00")
	if len(parts) != fields {
		return nil, fmt.Errorf("%w: no commit has changed %s", ErrRefused, folder)
	}
	info.Commit, info.AuthorName, info.AuthorEmail, info.Subject = parts[0], parts[1], parts[2], parts[3]
	committed, err := time.Parse(time.RFC3339, parts[4])
	if err != nil {
		return nil, fmt.Errorf("parsing committer date %q: %w", parts[4], err)
	}
	info.CommittedAt = committed.UTC().Format(time.RFC3339)

	prefix, err := git(ctx, folder, "rev-parse", "--show-prefix")
	if err != nil {
		return nil, err
	}
	info.Path = strings.TrimSuffix(prefix, "/")
	if info.Path == "" {
		info.Path = "."
	}

	if ref := os.Getenv("GITHUB_REF"); ref != "" {
		info.Ref = ref
	} else if ref, err := git(ctx, folder, "symbolic-ref", "-q", "HEAD"); err == nil {
		info.Ref = ref
	}
	return info, nil
}

// SourceURL is the source repository's URL, the first of: GITHUB_SERVER_URL/GITHUB_REPOSITORY,
// and, when folder is not empty, origin's URL normalised to https. It is "" when none is known.
func SourceURL(ctx context.Context, folder string) string {
	server, repo := os.Getenv("GITHUB_SERVER_URL"), os.Getenv("GITHUB_REPOSITORY")
	if server != "" && repo != "" {
		return strings.TrimSuffix(server, "/") + "/" + repo
	}
	if folder == "" {
		return ""
	}
	origin, err := git(ctx, folder, "remote", "get-url", "origin")
	if err != nil {
		return ""
	}
	return NormalizeRemote(origin)
}

var scpLike = regexp.MustCompile(`^(?:[^@/]+@)?([^:/]+):(.+)$`)

// NormalizeRemote turns a Git remote URL into a browsable https URL without credentials:
// "git@host:o/r.git" and "https://user:token@host/o/r.git" both become "https://host/o/r". It is
// "" for a remote it cannot place, such as a local path.
func NormalizeRemote(remote string) string {
	var host, p string
	if strings.Contains(remote, "://") {
		u, err := url.Parse(remote)
		if err != nil || u.Scheme == "file" || u.Host == "" {
			return ""
		}
		host, p = u.Hostname(), u.Path
		if port := u.Port(); port != "" && u.Scheme != "ssh" {
			host += ":" + port
		}
	} else if m := scpLike.FindStringSubmatch(remote); m != nil && !strings.HasPrefix(remote, "/") {
		host, p = m[1], m[2]
	} else {
		return ""
	}
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	return "https://" + host + "/" + p
}

func git(ctx context.Context, dir string, args ...string) (string, error) {
	//nolint:gosec // G204: running git on the user's folder is the point; args are never a shell.
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimRight(stdout.String(), "\n"), nil
}
