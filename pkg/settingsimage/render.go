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
	"path"
	"strings"
)

// MaxInputSize is the most a folder may hold, summed over its files' uncompressed bytes. The
// derived files/ view does not count.
const MaxInputSize = 10 << 20

// Tree is a rendered folder: slash-separated paths, each under objects/ or files/, to their
// bytes. It holds files only; directories are implied.
type Tree map[string][]byte

// RenderOptions are the writer's choices for the tree.
type RenderOptions struct {
	// NoFiles leaves out the files/ view.
	NoFiles bool
}

// Render reads the folder fsys (for a directory on disk, os.DirFS) into a tree, or refuses it.
func Render(fsys fs.FS, opts RenderOptions) (Tree, error) {
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
			return nil
		case d.Type()&fs.ModeSymlink != 0:
			return refuse(p, "a symlink; an image volume must not reach outside its folder")
		case !d.Type().IsRegular():
			return refuse(p, "not a regular file")
		}
		b, err := fs.ReadFile(fsys, p)
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
func RenderArchive(r io.Reader, root string, opts RenderOptions) (Tree, error) {
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
		return nil
	case tar.TypeSymlink, tar.TypeLink:
		return refuse(p, "a link; an image volume must not reach outside its folder")
	case tar.TypeReg:
	default:
		return refuse(p, "not a regular file")
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

// input is the folder as read: relative path to bytes.
type input struct {
	files map[string][]byte
	size  int64
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

func (in *input) render(opts RenderOptions) (Tree, error) {
	if len(in.files) == 0 {
		return nil, refuse(".", "the folder is empty; publishing it would roll every consumer to no settings")
	}
	tree := Tree{}
	for p, b := range in.files {
		tree["objects/"+p] = b
	}
	views, err := configMapFiles(in.files)
	if err != nil {
		return nil, err
	}
	if !opts.NoFiles {
		for p, b := range views {
			tree[p] = b
		}
	}
	if err := checkNoFileIsADirectory(tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// checkNoFileIsADirectory refuses a tree where one path is both a file and a parent of another:
// a tar cannot hold both. Only an archive can produce it.
func checkNoFileIsADirectory(tree Tree) error {
	for p := range tree {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			if _, ok := tree[dir]; ok {
				return refuse(strings.TrimPrefix(dir, "objects/"), "both a file and a directory")
			}
		}
	}
	return nil
}
