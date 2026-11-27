package handler

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// fixedGoFallbacks is the three locations tried after the version-sorted SDK
// glob, in this order. The stale go1.26.5 and go1.26.1 names are not among them
// (0005 amendment (a), N-c).
func fixedGoFallbacks(home string) []string {
	return []string{
		filepath.Join(home, ".local", "go", "bin", "go"),
		filepath.Join(home, "go", "bin", "go"),
		"/usr/local/go/bin/go",
	}
}

// resolveGoBinary picks the Go binary for pipeline build validation.
//
// Order: a non-empty override, unchecked; then the newest executable
// <home>/sdk/go*/bin/go; then fallbacks in the order given; then "go" on PATH.
// A directory name parses when it is exactly "go" plus MAJOR.MINOR or
// MAJOR.MINOR.PATCH, each a decimal with no sign and no leading '+'. A missing
// patch is 0. Parsed names are tried newest first. Equal versions tie by
// ascending full path. Names that do not parse rank below every parsed name
// and are ordered among themselves by descending directory-name text, so a
// release candidate is never preferred over a release. A candidate that is not
// an executable regular file is skipped. Windows is unchanged: the execute-bit
// check does not match there, so the lookup falls through to PATH.
func resolveGoBinary(override, home string, fallbacks []string) string {
	if override != "" {
		return override
	}
	var sdk []string
	if matches, err := filepath.Glob(filepath.Join(home, "sdk", "go*", "bin", "go")); err == nil {
		sdk = matches
	}
	sort.SliceStable(sdk, func(i, j int) bool {
		return sdkBefore(sdk[i], sdk[j])
	})
	for _, c := range append(sdk, fallbacks...) {
		if executableRegular(c) {
			return c
		}
	}
	return "go"
}

func executableRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular() && info.Mode()&0o111 != 0
}

// sdkBefore reports whether a should be tried before b. Parsed versions come
// first, newest first. Unparsed names follow, in descending directory-name text.
func sdkBefore(a, b string) bool {
	an, bn := sdkDirName(a), sdkDirName(b)
	amaj, amin, apatch, aok := parseSDKName(an)
	bmaj, bmin, bpatch, bok := parseSDKName(bn)
	if aok != bok {
		return aok
	}
	if aok {
		if amaj != bmaj {
			return amaj > bmaj
		}
		if amin != bmin {
			return amin > bmin
		}
		if apatch != bpatch {
			return apatch > bpatch
		}
		return a < b
	}
	if an != bn {
		return an > bn
	}
	return a < b
}

func sdkDirName(binPath string) string {
	return filepath.Base(filepath.Dir(filepath.Dir(binPath)))
}

func parseSDKName(name string) (major, minor, patch int, ok bool) {
	rest, found := strings.CutPrefix(name, "go")
	if !found || rest == "" {
		return 0, 0, 0, false
	}
	parts := strings.Split(rest, ".")
	if len(parts) != 2 && len(parts) != 3 {
		return 0, 0, 0, false
	}
	nums := [3]int{}
	for i, part := range parts {
		if part == "" || part[0] == '+' || part[0] == '-' {
			return 0, 0, 0, false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return 0, 0, 0, false
			}
		}
		n, err := strconv.Atoi(part)
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}
