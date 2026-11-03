// SPDX-License-Identifier: Apache-2.0

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"oras.land/oras-go/v2/content/oci"
)

// The format-v2.md §11 "configmap" case: the digest a commit of this folder must give.
const (
	knownConfigMap = `apiVersion: v1
kind: ConfigMap
metadata:
  name: shop-settings
data:
  settings.yaml: |
    maxScoops: 3
`
	knownSource = "https://github.com/ConfigButler/k8s-trail"
	knownDigest = "sha256:80b524374fdf0db1088308fe2941e6399c50b8d268891a4cd9e2b4e1775c88cd"
)

// repo is a scratch Git repository with configs/shop/resources holding the known ConfigMap, committed.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T) *repo {
	t.Helper()
	for _, v := range []string{"GITHUB_REF", "GITHUB_SERVER_URL", "GITHUB_REPOSITORY",
		"SETTINGS_IMAGE_USERNAME", "SETTINGS_IMAGE_PASSWORD"} {
		t.Setenv(v, "")
	}
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_AUTHOR_NAME", "Ada")
	t.Setenv("GIT_AUTHOR_EMAIL", "ada@example.com")
	t.Setenv("GIT_COMMITTER_NAME", "Ada")
	t.Setenv("GIT_COMMITTER_EMAIL", "ada@example.com")
	t.Setenv("GIT_COMMITTER_DATE", "2026-11-02T21:41:07+01:00")
	t.Setenv("GIT_AUTHOR_DATE", "2026-11-02T21:41:07+01:00")

	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.git("remote", "add", "origin", "git@github.com:ConfigButler/k8s-trail.git")
	r.write("configs/shop/resources/configmap.yaml", knownConfigMap)
	r.write("README.md", "hello\n")
	r.commit("feat: three scoops")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	cmd := exec.CommandContext(r.t.Context(), "git", append([]string{"-C", r.dir}, args...)...) //nolint:gosec // test
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(p, s string) {
	r.t.Helper()
	full := filepath.Join(r.dir, filepath.FromSlash(p))
	if err := os.MkdirAll(filepath.Dir(full), 0o750); err != nil {
		r.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(s), 0o600); err != nil {
		r.t.Fatal(err)
	}
}

func (r *repo) commit(msg string) string {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "-m", msg)
	return r.git("rev-parse", "HEAD")
}

// shop is the settings folder.
func (r *repo) shop() string { return filepath.Join(r.dir, "configs", "shop") }

// cli runs the command and returns its exit code, stdout and stderr.
func cli(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	code := run(t.Context(), args, strings.NewReader(""), &stdout, &stderr)
	return code, stdout.String(), stderr.String()
}

func cliJSON(t *testing.T, args ...string) result {
	t.Helper()
	code, stdout, stderr := cli(t, append(args, "--json")...)
	if code != exitOK {
		t.Fatalf("exit %d: %s", code, stderr)
	}
	var res result
	if err := json.Unmarshal([]byte(stdout), &res); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, stdout)
	}
	return res
}

func TestBuildFromGit(t *testing.T) {
	r := newRepo(t)
	head := r.git("rev-parse", "HEAD")
	layout := filepath.Join(t.TempDir(), "out")

	res := cliJSON(t, "build", r.shop(), "--oci-layout", layout, "--branch-tag", "Feature/X",
		"--tag", "v1")
	if res.Digest != knownDigest {
		t.Errorf("digest %s, want the known answer %s (source from origin, normalised)", res.Digest, knownDigest)
	}
	wantTags := []string{"sha-" + head[:7], "v1", "feature-x"}
	if strings.Join(res.Tags, " ") != strings.Join(wantTags, " ") {
		t.Errorf("tags %q, want %q", res.Tags, wantTags)
	}
	if res.Commit == nil || *res.Commit != head || res.Provenance == nil || res.Changed != nil || res.Skipped == nil {
		t.Errorf("%+v: want commit %s, a referrer, and no changed", res, head)
	}

	assertLayoutTags(t, layout, wantTags)

	// A commit to another folder changes nothing: same digest, same commit, same referrer.
	r.write("README.md", "changed\n")
	r.commit("docs: elsewhere")
	again := cliJSON(t, "build", r.shop())
	if again.Digest != res.Digest || *again.Commit != head || *again.Provenance != *res.Provenance {
		t.Errorf("after an unrelated commit: %+v, want the same as %+v", again, res)
	}

	// A commit to the folder moves the commit and the referrer, not necessarily the digest.
	r.write("configs/shop/raw/notes.txt", "more\n")
	next := r.commit("feat: notes")
	moved := cliJSON(t, "build", r.shop())
	if *moved.Commit != next || moved.Digest == res.Digest {
		t.Errorf("after a commit to the folder: %+v", moved)
	}
}

func assertLayoutTags(t *testing.T, layout string, tags []string) {
	t.Helper()
	store, err := oci.New(layout)
	if err != nil {
		t.Fatal(err)
	}
	for _, tag := range tags {
		desc, err := store.Resolve(t.Context(), tag)
		if err != nil || desc.Digest.String() != knownDigest {
			t.Errorf("layout tag %s: %v %v", tag, desc.Digest, err)
		}
	}
}

func TestBuildRefusals(t *testing.T) {
	tests := []struct {
		name   string
		setup  func(r *repo) []string
		wantIn string
	}{
		{
			name: "dirty: modified",
			setup: func(r *repo) []string {
				r.write("configs/shop/resources/configmap.yaml", knownConfigMap+"# edit\n")
				return []string{"build", r.shop()}
			},
			wantIn: "uncommitted",
		},
		{
			name: "dirty: untracked",
			setup: func(r *repo) []string {
				r.write("configs/shop/raw/new.txt", "x")
				return []string{"build", r.shop()}
			},
			wantIn: "uncommitted",
		},
		{
			name: "secret",
			setup: func(r *repo) []string {
				r.write("configs/shop/resources/s.yaml", "apiVersion: v1\nkind: Secret\nmetadata:\n  name: s\n")
				r.commit("oops")
				return []string{"build", r.shop()}
			},
			wantIn: "resources/s.yaml",
		},
		{
			name: "shallow clone",
			setup: func(r *repo) []string {
				clone := filepath.Join(t.TempDir(), "clone")
				r.git("clone", "-q", "--depth", "1", "file://"+r.dir, clone)
				return []string{"build", filepath.Join(clone, "configs", "shop")}
			},
			wantIn: "shallow",
		},
		{
			name: "not in Git",
			setup: func(*repo) []string {
				dir := t.TempDir()
				if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o600); err != nil {
					t.Fatal(err)
				}
				return []string{"build", dir}
			},
			wantIn: "not inside a Git work tree",
		},
		{
			name:   "invalid tag",
			setup:  func(r *repo) []string { return []string{"build", r.shop(), "--tag", "a/b"} },
			wantIn: "invalid OCI tag",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := tt.setup(newRepo(t))
			code, stdout, stderr := cli(t, args...)
			if code != exitRefused || !strings.Contains(stderr, tt.wantIn) {
				t.Errorf("exit %d, stderr %q; want %d naming %q", code, stderr, exitRefused, tt.wantIn)
			}
			if stdout != "" {
				t.Errorf("stdout %q on a refusal", stdout)
			}
		})
	}
}

func TestBuildEscapes(t *testing.T) {
	r := newRepo(t)
	r.write("configs/shop/raw/new.txt", "x")
	dirty := cliJSON(t, "build", r.shop(), "--allow-dirty")
	if dirty.Provenance != nil || dirty.Commit != nil || len(dirty.Tags) != 0 {
		t.Errorf("--allow-dirty: %+v, want no provenance and no sha- tag", dirty)
	}

	clone := filepath.Join(t.TempDir(), "clone")
	r.git("clone", "-q", "--depth", "1", "file://"+r.dir, clone)
	code, _, stderr := cli(t, "build", filepath.Join(clone, "configs", "shop"), "--allow-shallow")
	if code != exitOK || !strings.Contains(stderr, "warning: shallow clone") {
		t.Errorf("--allow-shallow: exit %d, stderr %q", code, stderr)
	}

	plain := t.TempDir()
	if err := os.Mkdir(filepath.Join(plain, "resources"), 0o750); err != nil {
		t.Fatal(err)
	}
	cm := filepath.Join(plain, "resources", "configmap.yaml")
	if err := os.WriteFile(cm, []byte(knownConfigMap), 0o600); err != nil {
		t.Fatal(err)
	}
	res := cliJSON(t, "build", plain, "--no-provenance", "--source", knownSource)
	if res.Digest != knownDigest || res.Provenance != nil {
		t.Errorf("--no-provenance outside Git: %+v", res)
	}

	t.Setenv("GITHUB_SERVER_URL", "https://github.com")
	t.Setenv("GITHUB_REPOSITORY", "ConfigButler/k8s-trail")
	t.Setenv("GITHUB_REF", "refs/heads/release")
	fromEnv := cliJSON(t, "build", plain, "--no-provenance")
	if fromEnv.Digest != knownDigest {
		t.Errorf("source from GITHUB_*: digest %s", fromEnv.Digest)
	}
}

func TestBuildPlainOutput(t *testing.T) {
	r := newRepo(t)
	code, stdout, _ := cli(t, "build", r.shop(), "--repository", "example.com/shop")
	if code != exitOK || stdout != "example.com/shop@"+knownDigest+"\n" {
		t.Errorf("exit %d, stdout %q", code, stdout)
	}
}

func TestBuildUsage(t *testing.T) {
	r := newRepo(t)
	for name, args := range map[string][]string{
		"no folder":           {"build"},
		"two folders":         {"build", r.dir, r.dir},
		"missing folder":      {"build", filepath.Join(r.dir, "nope")},
		"push without repo":   {"push", r.shop()},
		"unknown flag":        {"build", r.shop(), "--frobnicate"},
		"oci-layout on push":  {"push", r.shop(), "--repository", "x/y", "--oci-layout", "o"},
		"stdin without user":  {"push", r.shop(), "--repository", "x/y", "--password-stdin"},
		"invalid repository":  {"push", r.shop(), "--repository", "UPPER/Case"},
		"plain-http on build": {"build", r.shop(), "--plain-http"},
	} {
		t.Run(name, func(t *testing.T) {
			if code, _, stderr := cli(t, args...); code != exitUsage {
				t.Errorf("exit %d, want %d; stderr %q", code, exitUsage, stderr)
			}
		})
	}
}

func TestBuildSensitiveKind(t *testing.T) {
	r := newRepo(t)
	code, _, stderr := cli(t, "build", r.shop(), "--sensitive-kind", "ConfigMap")
	if code != exitRefused || !strings.Contains(stderr, "ConfigMap shop-settings is not SOPS-encrypted") {
		t.Errorf("exit %d, stderr %q", code, stderr)
	}
}

// A folder gitops-reverser writes into: its marker and a README beside resources/. They are
// skipped and named, and editing them changes nothing: not the digest, not the commit, and an
// uncommitted edit does not make the folder dirty.
func TestBuildSkipsKnownTopLevelFiles(t *testing.T) {
	r := newRepo(t)
	r.write("configs/shop/.gittargetignore", "")
	r.write("configs/shop/README.md", "# shop\n")
	head := r.commit("chore: reverser marker")
	before := cliJSON(t, "build", r.shop())
	if before.Digest != knownDigest {
		t.Errorf("digest %s, want the known answer %s: skipped files must not reach the image",
			before.Digest, knownDigest)
	}
	if strings.Join(before.Skipped, " ") != ".gittargetignore README.md" {
		t.Errorf("skipped %q", before.Skipped)
	}
	if *before.Commit == head {
		t.Errorf("commit %s is the one that only added skipped files", head)
	}

	r.write("configs/shop/README.md", "# shop, edited\n")
	code, _, stderr := cli(t, "build", r.shop())
	if code != exitOK || !strings.Contains(stderr, "warning: skipped README.md") {
		t.Errorf("an uncommitted edit to a skipped file: exit %d, stderr %q", code, stderr)
	}
	r.commit("docs: edit the README")
	after := cliJSON(t, "build", r.shop())
	if after.Digest != before.Digest || *after.Commit != *before.Commit || *after.Provenance != *before.Provenance {
		t.Errorf("after editing only a skipped file: %+v, want the same as %+v", after, before)
	}

	// A resource one level too high is still refused, committed or not.
	r.write("configs/shop/menu.yaml", knownConfigMap)
	code, _, stderr = cli(t, "build", r.shop())
	if code != exitRefused || !strings.Contains(stderr, "refused menu.yaml") {
		t.Errorf("menu.yaml beside resources/: exit %d, stderr %q", code, stderr)
	}
}
