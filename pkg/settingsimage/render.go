// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"maps"
	"path"
	"slices"
	"sort"
	"strings"
)

// MaxInputSize is the most a folder may hold, summed over its files' uncompressed bytes. The
// derived configmaps/ files do not count.
const MaxInputSize = 10 << 20

// Tree is a rendered folder: slash-separated paths, each under resources/, raw/ or configmaps/,
// to their bytes. It holds files only; directories are implied.
type Tree map[string][]byte

// Rendered is a folder read into a tree, and what was left out of it.
type Rendered struct {
	// Tree is what goes into the image.
	Tree Tree
	// Skipped are the known files at the top of the folder that were left out (format-v2.md §2),
	// sorted. The writer names each one, so its author knows it is not in the image.
	Skipped []string
}

// RenderOptions are the writer's choices for the tree.
type RenderOptions struct {
	// SensitiveKinds are kinds that must arrive SOPS-encrypted, as "group/Kind", or "Kind" for
	// the core group. A Secret must always be SOPS-encrypted.
	SensitiveKinds []string
}

// Render reads the folder fsys (for a directory on disk, os.DirFS) into a tree, or refuses it.
func Render(fsys fs.FS, opts RenderOptions) (*Rendered, error) {
	in := input{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Name() == ".git" && p != "." {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		switch {
		case d.IsDir():
			return in.addDir(p)
		case d.Type()&fs.ModeSymlink != 0:
			return refuse(p, "a symlink; an image volume must not reach outside its folder")
		case !d.Type().IsRegular():
			return refuse(p, "not a regular file")
		case in.skip(p):
			return nil
		}
		b, err := in.readFile(fsys, p)
		if err != nil {
			return err
		}
		return in.add(p, b)
	})
	if err != nil {
		return nil, err
	}
	return in.render(opts)
}

// RenderArchive reads the folder at root inside a tar or tar+gzip archive (a Flux artifact is
// one) into a tree, or refuses it. Entries outside root are ignored. root is slash-separated;
// "" or "." is the whole archive.
func RenderArchive(r io.Reader, root string, opts RenderOptions) (*Rendered, error) {
	br := bufio.NewReader(r)
	r = br
	if magic, _ := br.Peek(len(gzipMagic)); string(magic) == gzipMagic {
		zr, err := gzip.NewReader(br)
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		defer func() { _ = zr.Close() }()
		r = zr
	}

	root = strings.Trim(path.Clean("/"+root), "/")
	in := input{}
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("reading archive: %w", err)
		}
		if err := in.addEntry(tr, hdr, root); err != nil {
			return nil, err
		}
	}
	return in.render(opts)
}

// gzipMagic starts every gzip stream.
const gzipMagic = "\x1f\x8b"

// addEntry adds one archive entry if it is a file under root, and refuses what must be refused.
func (in *input) addEntry(tr *tar.Reader, hdr *tar.Header, root string) error {
	p, ok, err := archivePath(hdr.Name, root)
	if err != nil {
		return err
	}
	if !ok || p == "" || hasGitComponent(p) {
		return nil
	}
	switch hdr.Typeflag {
	case tar.TypeDir:
		return in.addDir(p)
	case tar.TypeSymlink, tar.TypeLink:
		return refuse(p, "a link; an image volume must not reach outside its folder")
	case tar.TypeReg:
	default:
		return refuse(p, "not a regular file")
	}
	if in.skip(p) {
		return nil
	}
	if hdr.Size > MaxInputSize-in.size {
		return refuse(".", "more than %d bytes in total", MaxInputSize)
	}
	b, err := io.ReadAll(tr)
	if err != nil {
		return fmt.Errorf("reading archive: %w", err)
	}
	return in.add(p, b)
}

// archivePath maps an entry name to its path relative to root. ok is false for an entry outside
// root, which is skipped; a name that escapes the archive is refused wherever it is.
func archivePath(name, root string) (string, bool, error) {
	clean := strings.TrimSuffix(name, "/")
	for strings.HasPrefix(clean, "./") {
		clean = clean[2:]
	}
	if clean == "." {
		clean = ""
	}
	if strings.HasPrefix(clean, "/") {
		return "", false, refuse(name, "an absolute path escapes the archive")
	}
	for c := range strings.SplitSeq(clean, "/") {
		if c == ".." {
			return "", false, refuse(name, "a path that escapes the archive")
		}
	}
	clean = path.Clean("/" + clean)[1:]
	switch {
	case root == "":
		return clean, true, nil
	case clean == root:
		return "", true, nil
	case strings.HasPrefix(clean, root+"/"):
		return clean[len(root)+1:], true, nil
	default:
		return "", false, nil
	}
}

func hasGitComponent(p string) bool {
	for c := range strings.SplitSeq(p, "/") {
		if c == ".git" {
			return true
		}
	}
	return false
}

// IsKnownTopLevelFile reports whether a file of this name, at the top of a settings folder, is
// skipped rather than refused (format-v2.md §2). The list is closed: the files a repository
// commonly keeps beside its content (a README, a licence, Git and SOPS settings, gitops-reverser's
// .gittargetignore). Any other file at the top is refused.
func IsKnownTopLevelFile(name string) bool {
	switch name {
	case ".gittargetignore", ".sops.yaml", ".gitignore", ".gitattributes", ".gitkeep", ".keep",
		"LICENSE", "LICENSE.txt", "LICENCE", "LICENCE.txt", "COPYING", "COPYING.txt", "NOTICE", "NOTICE.txt":
		return true
	}
	return strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".markdown")
}

// input is the folder as read: relative path to bytes.
type input struct {
	files   map[string][]byte
	dirs    map[string]bool // directories the input names, even empty ones
	skipped []string
	size    int64
}

// addDir records a directory, so render can refuse one that no file reveals: an empty resource/
// typo, or an archive's directory entry at the path of a file.
func (in *input) addDir(p string) error {
	if p == "." || p == "" {
		return nil
	}
	if in.dirs == nil {
		in.dirs = map[string]bool{}
	}
	in.dirs[p] = true
	return nil
}

// checkDirs refuses a directory that holds no file to refuse: at the top of the folder only
// resources/ and raw/ may be one (format-v2.md §2), and no path is both a file and a directory.
func (in *input) checkDirs() error {
	for _, d := range slices.Sorted(maps.Keys(in.dirs)) {
		if _, ok := in.files[d]; ok {
			return refuse(d, "both a file and a directory")
		}
		if !strings.Contains(d, "/") && d != ResourcesDir && d != RawDir {
			return refuse(d, "a directory beside resources/ and raw/; only those two hold settings")
		}
	}
	return nil
}

// readFile reads p, but never more than the size limit allows: a huge file is refused, not
// loaded into memory first.
func (in *input) readFile(fsys fs.FS, p string) ([]byte, error) {
	f, err := fsys.Open(p)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	remaining := MaxInputSize - in.size
	b, err := io.ReadAll(io.LimitReader(f, remaining+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > remaining {
		return nil, refuse(".", "more than %d bytes in total", MaxInputSize)
	}
	return b, nil
}

// skip reports whether p is a known top-level file, and records it if so. A skipped file is never
// read, and does not count towards the size limit or make the folder non-empty.
func (in *input) skip(p string) bool {
	if strings.Contains(p, "/") || !IsKnownTopLevelFile(p) {
		return false
	}
	in.skipped = append(in.skipped, p)
	return true
}

func (in *input) add(p string, b []byte) error {
	if in.files == nil {
		in.files = map[string][]byte{}
	}
	if _, dup := in.files[p]; dup {
		return refuse(p, "the path appears twice")
	}
	in.size += int64(len(b))
	if in.size > MaxInputSize {
		return refuse(".", "more than %d bytes in total", MaxInputSize)
	}
	in.files[p] = b
	return nil
}

func (in *input) render(opts RenderOptions) (*Rendered, error) {
	if len(in.files) == 0 {
		return nil, refuse(".", "the folder is empty: no file under resources/ or raw/; "+
			"publishing it would roll every consumer to no settings")
	}
	tree := Tree{}
	for p, b := range in.files {
		top, _, nested := strings.Cut(p, "/")
		if !nested || (top != ResourcesDir && top != RawDir) {
			return nil, refuse(p, "not under resources/ or raw/; put Kubernetes resources in resources/ "+
				"and any other file in raw/ (only a known file such as README.md may sit beside them)")
		}
		tree[p] = b
	}
	if err := in.checkDirs(); err != nil {
		return nil, err
	}
	configMaps, err := renderResources(in.files, opts)
	if err != nil {
		return nil, err
	}
	for p, b := range configMaps {
		tree[p] = b
	}
	if err := checkNoFileIsADirectory(tree); err != nil {
		return nil, err
	}
	sort.Strings(in.skipped)
	return &Rendered{Tree: tree, Skipped: in.skipped}, nil
}

// checkTree refuses a tree a layer cannot safely hold. Build is public, and its caller need not
// have come through Render: every path must be clean, relative and inside the image.
func checkTree(tree Tree) error {
	for p := range tree {
		if p == "." || !fs.ValidPath(p) {
			return refuse(p, "not a clean relative path inside the image "+
				"(no leading /, no . or .. component, no empty component)")
		}
	}
	return checkNoFileIsADirectory(tree)
}

// checkNoFileIsADirectory refuses a tree where one path is both a file and a parent of another:
// a tar cannot hold both. Only an archive, or a hand-made Tree, can produce it.
func checkNoFileIsADirectory(tree Tree) error {
	for p := range tree {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if _, ok := tree[dir]; ok {
				return refuse(dir, "both a file and a directory")
			}
		}
	}
	return nil
}
