// SPDX-License-Identifier: Apache-2.0

package buildinfo_test

import (
	"runtime"
	"testing"

	"github.com/ConfigButler/settings-image/internal/buildinfo"
)

func TestStringMarksADirtyTree(t *testing.T) {
	info := buildinfo.Info{
		Version:   "0.1.0",
		GitCommit: "abc1234",
		Dirty:     true,
		BuildDate: "2026-11-02T00:00:00Z",
		GoVersion: "go1.27.3",
	}
	want := "version=0.1.0 commit=abc1234-dirty built=2026-11-02T00:00:00Z go=go1.27.3"
	if got := info.String(); got != want {
		t.Fatalf("String() = %q, want %q", got, want)
	}
}

func TestGetReportsTheRunningGo(t *testing.T) {
	if got := buildinfo.Get().GoVersion; got != runtime.Version() {
		t.Fatalf("GoVersion = %q, want %q", got, runtime.Version())
	}
}
