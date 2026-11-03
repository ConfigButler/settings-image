// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/oci"

	"github.com/ConfigButler/settings-image/internal/gitinfo"
	"github.com/ConfigButler/settings-image/pkg/settingsimage"
)

// buildFlags are the flags of `build` and `push` (docs/cli.md §2).
type buildFlags struct {
	push          bool
	folder        string
	repository    string
	ociLayout     string
	tags          stringList
	sensitive     stringList
	branchTag     string
	timestampTag  string
	source        string
	noProvenance  bool
	allowDirty    bool
	allowShallow  bool
	author        string
	plainHTTP     bool
	username      string
	passwordStdin bool
	json          bool
}

// result is the --json output (docs/cli.md §5).
type result struct {
	Repository string   `json:"repository,omitempty"`
	Digest     string   `json:"digest"`
	Reference  string   `json:"reference,omitempty"`
	Tags       []string `json:"tags"`
	Provenance *string  `json:"provenance"`
	Commit     *string  `json:"commit"`
	Skipped    []string `json:"skipped"`
	Changed    *bool    `json:"changed,omitempty"`
}

// refusedError is a refusal found by the CLI itself, such as a dirty folder.
type refusedError struct{ msg string }

func (e *refusedError) Error() string { return e.msg }

// usageError is a missing or malformed argument.
type usageError struct{ msg string }

func (e *usageError) Error() string { return e.msg }

func runBuild(ctx context.Context, push bool, args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	f, err := parseBuildFlags(push, args, stderr)
	if err != nil {
		return report(stderr, err)
	}
	res, err := buildAndPush(ctx, f, stdin, stderr)
	if err != nil {
		return report(stderr, err)
	}
	if f.json {
		enc := json.NewEncoder(stdout)
		if err := enc.Encode(res); err != nil {
			return report(stderr, err)
		}
		return exitOK
	}
	if res.Reference != "" {
		_, _ = fmt.Fprintln(stdout, res.Reference)
	} else {
		_, _ = fmt.Fprintln(stdout, res.Digest)
	}
	return exitOK
}

func parseBuildFlags(push bool, args []string, stderr io.Writer) (*buildFlags, error) {
	name := "build"
	if push {
		name = "push"
	}
	f := &buildFlags{push: push}
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&f.repository, "repository", "", "where to push, host/path without a tag")
	if !push {
		fs.StringVar(&f.ociLayout, "oci-layout", "", "write an OCI image layout directory")
	}
	fs.Var(&f.tags, "tag", "an extra tag (repeatable)")
	fs.StringVar(&f.branchTag, "branch-tag", "", "add the sanitised branch tag, pushed last")
	fs.StringVar(&f.timestampTag, "timestamp-tag", "",
		"add <branch>-<unix commit time>-<sha7>, a tag that never moves and sorts by commit time")
	fs.StringVar(&f.source, "source", "", "the source repository URL")
	fs.Var(&f.sensitive, "sensitive-kind", "a kind that must be SOPS-encrypted, as group/Kind (repeatable)")
	fs.BoolVar(&f.noProvenance, "no-provenance", false, "build no referrer, and skip Git entirely")
	fs.BoolVar(&f.allowDirty, "allow-dirty", false, "build a folder with uncommitted changes, without provenance")
	fs.BoolVar(&f.allowShallow, "allow-shallow", false, "accept a shallow clone; provenance names HEAD")
	fs.StringVar(&f.author, "provenance-author", authorNone,
		"how much of the commit author goes into provenance: none, name or full")
	if push {
		fs.BoolVar(&f.plainHTTP, "plain-http", false, "talk HTTP to the registry")
		fs.StringVar(&f.username, "username", "", "registry username")
		fs.BoolVar(&f.passwordStdin, "password-stdin", false, "read the registry password from stdin")
	}
	fs.BoolVar(&f.json, "json", false, "print the result as JSON")

	positional, err := parseInterspersed(fs, args)
	if err != nil {
		return nil, &usageError{msg: err.Error()}
	}
	if len(positional) != 1 {
		return nil, &usageError{
			msg: fmt.Sprintf("%s takes exactly one folder, got %d arguments", name, len(positional)),
		}
	}
	f.folder = positional[0]
	if msg := f.invalid(); msg != "" {
		return nil, &usageError{msg: msg}
	}
	return f, nil
}

// invalid says what is wrong with a combination of flags, or "".
func (f *buildFlags) invalid() string {
	switch {
	case f.push && f.repository == "":
		return "push needs --repository"
	case f.passwordStdin && f.username == "":
		return "--password-stdin needs --username"
	case f.timestampTag != "" && (f.noProvenance || f.allowDirty):
		return "--timestamp-tag needs the commit that last changed the folder; " +
			"it cannot be used with --no-provenance or --allow-dirty"
	case f.author != authorNone && f.author != authorName && f.author != authorFull:
		return fmt.Sprintf("--provenance-author is none, name or full, not %q", f.author)
	}
	if st, err := os.Stat(f.folder); err != nil || !st.IsDir() {
		return fmt.Sprintf("%s is not a directory", f.folder)
	}
	return ""
}

// parseInterspersed parses flags that may come before or after the positional arguments.
func parseInterspersed(fs *flag.FlagSet, args []string) ([]string, error) {
	var positional []string
	for {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}
		args = fs.Args()
		if len(args) == 0 {
			return positional, nil
		}
		positional = append(positional, args[0])
		args = args[1:]
	}
}

func buildAndPush(ctx context.Context, f *buildFlags, stdin io.Reader, stderr io.Writer) (*result, error) {
	for _, tag := range f.tags {
		if err := settingsimage.ValidateTag(tag); err != nil {
			return nil, err
		}
	}
	info, source, err := readGit(ctx, f, stderr)
	if err != nil {
		return nil, err
	}

	rendered, err := render(f, stderr)
	if err != nil {
		return nil, err
	}
	img, err := settingsimage.Build(ctx, rendered.Tree, settingsimage.BuildOptions{Source: source})
	if err != nil {
		return nil, err
	}
	res := &result{
		Repository: f.repository,
		Digest:     img.Manifest.Digest.String(),
		Tags:       tagsFor(f, info),
		Skipped:    append([]string{}, rendered.Skipped...),
	}
	if info != nil {
		if err := img.AttachProvenance(ctx, provenance(info, source, f.author)); err != nil {
			return nil, err
		}
		prov, commit := img.Provenance.Digest.String(), info.Commit
		res.Provenance, res.Commit = &prov, &commit
	}

	target, err := openTarget(f, stdin)
	if err != nil {
		return nil, err
	}
	if target != nil {
		pushed, err := img.Push(ctx, target, res.Tags)
		if err != nil {
			return nil, err
		}
		if f.push {
			res.Changed = &pushed.Changed
		}
	}
	if f.repository != "" {
		res.Reference = f.repository + "@" + res.Digest
	}
	return res, nil
}

// render reads the folder, and names each skipped file in a warning.
func render(f *buildFlags, stderr io.Writer) (*settingsimage.Rendered, error) {
	rendered, err := settingsimage.Render(os.DirFS(f.folder), settingsimage.RenderOptions{SensitiveKinds: f.sensitive})
	if err != nil {
		return nil, err
	}
	for _, p := range rendered.Skipped {
		_, _ = fmt.Fprintf(stderr, "warning: skipped %s: a known file beside resources/ and raw/, "+
			"not part of the image\n", p)
	}
	return rendered, nil
}

// tagsFor is the tags in push order: sha- and timestamp, the caller's, then the moving branch tag.
func tagsFor(f *buildFlags, info *gitinfo.Info) []string {
	tags := []string{}
	if info != nil {
		tags = append(tags, settingsimage.ShaTag(info.Commit))
		if f.timestampTag != "" {
			tags = append(tags, settingsimage.TimestampTag(f.timestampTag, info.Committed, info.Commit))
		}
	}
	tags = append(tags, f.tags...)
	if f.branchTag != "" {
		tags = append(tags, settingsimage.BranchTag(f.branchTag))
	}
	return tags
}

// readGit returns the provenance (nil when there is none) and the source URL.
func readGit(ctx context.Context, f *buildFlags, stderr io.Writer) (*gitinfo.Info, string, error) {
	source := f.source
	if f.noProvenance {
		if source == "" {
			source = gitinfo.SourceURL(ctx, "")
		}
		return nil, source, nil
	}
	if err := gitinfo.CheckWorkTree(ctx, f.folder); err != nil {
		return nil, "", err
	}
	if source == "" {
		source = gitinfo.SourceURL(ctx, f.folder)
	}
	if f.allowDirty {
		_, _ = fmt.Fprintln(stderr, "warning: --allow-dirty: no provenance and no sha- tag")
		return nil, source, nil
	}
	dirty, err := gitinfo.Dirty(ctx, f.folder)
	if err != nil {
		return nil, "", err
	}
	if dirty {
		return nil, "", &refusedError{msg: fmt.Sprintf(
			"refused %s: the folder has uncommitted changes; commit them, or use --allow-dirty", f.folder)}
	}
	info, err := gitinfo.Read(ctx, f.folder, gitinfo.Options{AllowShallow: f.allowShallow})
	if err != nil {
		return nil, "", err
	}
	if info.Shallow {
		_, _ = fmt.Fprintf(stderr, "warning: shallow clone: provenance names HEAD (%s), "+
			"which may not be the commit that last changed %s\n", info.Commit, f.folder)
	}
	return info, source, nil
}

// The values of --provenance-author. A referrer is public wherever the image is, so the default
// copies nothing about the author out of the repository (docs/format-v2.md §9).
const (
	authorNone = "none"
	authorName = "name"
	authorFull = "full"
)

func provenance(info *gitinfo.Info, source, author string) settingsimage.Provenance {
	p := settingsimage.Provenance{
		Commit:      info.Commit,
		CommittedAt: info.CommittedAt,
		Path:        info.Path,
		Ref:         info.Ref,
		Repository:  source,
		Subject:     info.Subject,
	}
	switch {
	case author == authorName && info.AuthorName != "":
		p.Author = &settingsimage.Author{Name: info.AuthorName}
	case author == authorFull && (info.AuthorName != "" || info.AuthorEmail != ""):
		p.Author = &settingsimage.Author{Email: info.AuthorEmail, Name: info.AuthorName}
	}
	return p
}

// openTarget is the registry for push, the OCI layout for build --oci-layout, or nil.
func openTarget(f *buildFlags, stdin io.Reader) (oras.Target, error) {
	if !f.push {
		if f.ociLayout == "" {
			return nil, nil //nolint:nilnil // no target: build writes nothing
		}
		store, err := oci.New(f.ociLayout)
		if err != nil {
			return nil, fmt.Errorf("opening OCI layout %s: %w", f.ociLayout, err)
		}
		return store, nil
	}
	opts := settingsimage.RepositoryOptions{PlainHTTP: f.plainHTTP}
	switch {
	case f.passwordStdin:
		b, err := io.ReadAll(stdin)
		if err != nil {
			return nil, fmt.Errorf("reading the password from stdin: %w", err)
		}
		opts.Username, opts.Password = f.username, strings.TrimRight(string(b), "\r\n")
	case os.Getenv("SETTINGS_IMAGE_PASSWORD") != "":
		opts.Username, opts.Password = os.Getenv("SETTINGS_IMAGE_USERNAME"), os.Getenv("SETTINGS_IMAGE_PASSWORD")
	}
	repo, err := settingsimage.NewRepository(f.repository, opts)
	if err != nil {
		return nil, &usageError{msg: err.Error()}
	}
	return repo, nil
}

// report prints err and returns its exit code (docs/cli.md §6).
func report(stderr io.Writer, err error) int {
	if errors.Is(err, flag.ErrHelp) {
		return exitOK
	}
	_, _ = fmt.Fprintf(stderr, "settings-image: %v\n", err)
	var (
		usage   *usageError
		refused *refusedError
		refusal *settingsimage.RefusalError
	)
	switch {
	case errors.As(err, &usage):
		return exitUsage
	case errors.As(err, &refused), errors.As(err, &refusal),
		errors.Is(err, settingsimage.ErrInvalidTag), errors.Is(err, gitinfo.ErrRefused):
		return exitRefused
	default:
		return exitError
	}
}

// stringList is a repeatable string flag.
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }

func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}
