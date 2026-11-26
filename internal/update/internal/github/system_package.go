package github

import (
	"strconv"
	"strings"

	"sidravia/internal/release"
)

// A system package has its own revision within the product tag. Only the
// exact package and architecture qualify. apt/dpkg or RPM owns installation.
func systemPackageAssets(assets []asset, version string, selection release.Selection, manager string) []asset {
	packageName := "sidravia"
	if strings.HasPrefix(selection.Kind, "core-") {
		packageName = "sidravia-core"
	}
	nativeVersion := strings.Replace(version, "-", "~", 1)
	var prefix, legacyPrefix, suffix string
	switch manager {
	case "deb":
		prefix = packageName + "_" + nativeVersion + "-"
		suffix = "_" + selection.Arch + ".deb"
	case "rpm":
		prefix = packageName + "-" + nativeVersion + "-"
		legacyPrefix = packageName + "-" + version + "-"
		arch := "x86_64"
		if selection.Arch == "arm64" {
			arch = "aarch64"
		}
		suffix = "." + arch + ".rpm"
	}

	var matches []asset
	var highest uint64
	for _, a := range assets {
		revision, ok := strings.CutPrefix(a.Name, prefix)
		if !ok && legacyPrefix != "" {
			revision, ok = strings.CutPrefix(a.Name, legacyPrefix)
		}
		if !ok {
			continue
		}
		revision, ok = strings.CutSuffix(revision, suffix)
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(revision, 10, 64)
		if err != nil || n == 0 || strconv.FormatUint(n, 10) != revision || n < highest {
			continue
		}
		if n > highest {
			highest = n
			matches = matches[:0]
		}
		matches = append(matches, a)
	}
	return matches
}
