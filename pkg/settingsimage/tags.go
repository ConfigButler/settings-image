// SPDX-License-Identifier: Apache-2.0

package settingsimage

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// maxTagLength is the OCI distribution spec's limit for a tag.
const maxTagLength = 128

// shortCommitLength is how much of a commit the sha- and timestamp tags carry.
const shortCommitLength = 7

// maxTimestampBranchLength leaves room in a timestamp tag for "-", ten digits of Unix seconds,
// "-" and seven of the commit: 109 + 19 = 128.
const maxTimestampBranchLength = maxTagLength - 1 - 10 - 1 - shortCommitLength

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

// TimestampTag is the tag that never moves and sorts by commit time (format-v2.md §10):
// "<branch>-<committedAt as Unix seconds>-<first 7 of the commit>", for the commit that last
// changed the folder. The branch is sanitised as by [BranchTag] and cut to 109 characters, so the
// tag stays within 128. Flux's ImagePolicy follows it with a numerical policy.
func TimestampTag(branch string, committedAt time.Time, commit string) string {
	b := BranchTag(branch)
	if len(b) > maxTimestampBranchLength {
		b = b[:maxTimestampBranchLength]
	}
	return fmt.Sprintf("%s-%d-%s", b, committedAt.Unix(), shortCommit(commit))
}

func shortCommit(commit string) string {
	if len(commit) > shortCommitLength {
		return commit[:shortCommitLength]
	}
	return commit
}

// ShaTag is the tag for the commit that last changed the folder: "sha-" and its first 7
// characters.
func ShaTag(commit string) string {
	return "sha-" + shortCommit(commit)
}
