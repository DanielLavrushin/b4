package config

import (
	"os"
	"strings"
)

var standardBinPaths = []string{
	"/opt/sbin",
	"/opt/bin",
	"/usr/local/sbin",
	"/usr/local/bin",
	"/usr/sbin",
	"/usr/bin",
	"/sbin",
	"/bin",
}

func ExtendedPATH(current string) string {
	seen := make(map[string]struct{})
	parts := make([]string, 0, len(standardBinPaths)+8)

	add := func(p string) {
		if p == "" {
			return
		}
		if _, ok := seen[p]; ok {
			return
		}
		seen[p] = struct{}{}
		parts = append(parts, p)
	}

	for _, p := range strings.Split(current, ":") {
		add(p)
	}
	for _, p := range standardBinPaths {
		add(p)
	}

	return strings.Join(parts, ":")
}

func ApplyPATH() string {
	full := ExtendedPATH(os.Getenv("PATH"))
	os.Setenv("PATH", full)
	return full
}
