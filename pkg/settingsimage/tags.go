// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
)

// maxTagLength is the OCI distribution spec's limit for a tag.
const maxTagLength = 128

// shortCommitLength is how much of a commit the sha- tag carries.
const shortCommitLength = 7

// ErrInvalidTag is wrapped by [ValidateTag]'s error.
var ErrInvalidTag = errors.New("invalid OCI tag")

var (
	tagRE         = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9._-]{0,127}$`)
	branchUnsafe  = regexp.MustCompile(`[^a-z0-9_.-]`)
	branchLeading = regexp.MustCompile(`^[.-]+`)
)

// ValidateTag refuses a string that is not a valid OCI tag. A tag is never rewritten.
func ValidateTag(tag string) error {
	if !tagRE.MatchString(tag) {
		return fmt.Errorf("%w: %q", ErrInvalidTag, tag)
	}
	return nil
}

// BranchTag sanitises a branch name into a tag (format-v2.md §10): lowercased, every character
// outside [a-z0-9_.-] replaced by "-", leading "." and "-" removed, at most 128 characters, and
// "branch" if nothing is left. "feature/Card" becomes "feature-card".
func BranchTag(branch string) string {
	t := branchUnsafe.ReplaceAllString(strings.ToLower(branch), "-")
	t = branchLeading.ReplaceAllString(t, "")
	if len(t) > maxTagLength {
		t = t[:maxTagLength]
	}
	if t == "" {
		return "branch"
	}
	return t
}

// ShaTag is the tag for the commit that last changed the folder: "sha-" and its first 7
// characters.
func ShaTag(commit string) string {
	if len(commit) > shortCommitLength {
		commit = commit[:shortCommitLength]
	}
	return "sha-" + commit
}
