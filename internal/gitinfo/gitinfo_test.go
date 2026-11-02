// SPDX-License-Identifier: Apache-2.0

package gitinfo_test

import (
	"testing"

	"github.com/ConfigButler/settings-image/internal/gitinfo"
)

func TestNormalizeRemote(t *testing.T) {
	for in, want := range map[string]string{ //nolint:gosec // G101: test URLs, not credentials.
		"git@github.com:ConfigButler/k8s-trail.git":            "https://github.com/ConfigButler/k8s-trail",
		"https://github.com/ConfigButler/k8s-trail.git":        "https://github.com/ConfigButler/k8s-trail",
		"https://x-access-token:secret@github.com/o/r.git":     "https://github.com/o/r",
		"ssh://git@github.com:22/o/r.git":                      "https://github.com/o/r",
		"https://gitea.example.com:3000/o/r/":                  "https://gitea.example.com:3000/o/r",
		"github.com:o/r":                                       "https://github.com/o/r",
		"/srv/git/repo.git":                                    "",
		"file:///srv/git/repo.git":                             "",
		"https://user@dev.azure.com/org/project/_git/settings": "https://dev.azure.com/org/project/_git/settings",
	} {
		if got := gitinfo.NormalizeRemote(in); got != want {
			t.Errorf("NormalizeRemote(%q) = %q, want %q", in, got, want)
		}
	}
}
