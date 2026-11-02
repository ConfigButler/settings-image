// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"path"
	"sort"
	"time"
)

const (
	dirMode  = 0o755
	fileMode = 0o644 // forced: an image volume is not mounted noexec (format-v2.md §6)
)

// layer is the tree as format-v2.md §6's deterministic tar+gzip.
type layer struct {
	tar  []byte // uncompressed; its sha256 is the diffID
	gzip []byte // its sha256 is the layer digest
}

func buildLayer(tree Tree) (*layer, error) {
	entries, isDir := layerEntries(tree)
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	for _, p := range entries {
		hdr := &tar.Header{ModTime: time.Unix(0, 0)}
		if isDir[p] {
			hdr.Typeflag, hdr.Name, hdr.Mode = tar.TypeDir, p+"/", dirMode
		} else {
			hdr.Typeflag, hdr.Name, hdr.Mode, hdr.Size = tar.TypeReg, p, fileMode, int64(len(tree[p]))
		}
		hdr.Format = headerFormat(hdr)
		if err := tw.WriteHeader(hdr); err != nil {
			return nil, fmt.Errorf("writing tar header for %s: %w", p, err)
		}
		if !isDir[p] {
			if _, err := tw.Write(tree[p]); err != nil {
				return nil, fmt.Errorf("writing %s: %w", p, err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		return nil, fmt.Errorf("closing tar: %w", err)
	}
	gz, err := gzipBytes(tarBuf.Bytes())
	if err != nil {
		return nil, err
	}
	return &layer{tar: tarBuf.Bytes(), gzip: gz}, nil
}

// layerEntries is every path of the layer, files and their parent directories, sorted by the
// plain path (format-v2.md §6).
func layerEntries(tree Tree) ([]string, map[string]bool) {
	isDir := map[string]bool{}
	for p := range tree {
		for dir := path.Dir(p); dir != "."; dir = path.Dir(dir) {
			isDir[dir] = true
		}
	}
	entries := make([]string, 0, len(tree)+len(isDir))
	for p := range tree {
		entries = append(entries, p)
	}
	for dir := range isDir {
		entries = append(entries, dir)
	}
	sort.Strings(entries)
	return entries, isDir
}

// gzipBytes compresses at the pinned level, with an empty header: no name, no time.
func gzipBytes(b []byte) ([]byte, error) {
	var gzBuf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&gzBuf, gzip.BestCompression)
	if err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	if _, err := zw.Write(b); err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzip: %w", err)
	}
	return gzBuf.Bytes(), nil
}

// headerFormat is USTAR, or PAX for a header USTAR cannot encode (a long or non-ASCII path).
// archive/tar refuses a forced USTAR header it cannot encode, so a trial write decides.
func headerFormat(hdr *tar.Header) tar.Format {
	trial := *hdr
	trial.Format = tar.FormatUSTAR
	trial.Size = 0
	if err := tar.NewWriter(io.Discard).WriteHeader(&trial); err != nil {
		return tar.FormatPAX
	}
	return tar.FormatUSTAR
}
