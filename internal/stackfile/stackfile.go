// Package stackfile keeps the installer's compose file in step with what runs.
package stackfile

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"modularnost/internal/docker"
)

var imageLine = regexp.MustCompile(`^(\s*image:\s*)(\S+)(\s*)$`)

// Retag rewrites every image line that names the same repository as image to
// image itself, and reports whether the file changed. Other images, such as
// Traefik, are left alone.
func Retag(path, image string) (bool, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return false, err
	}
	repo := docker.ImageRepo(image)
	lines := strings.Split(string(raw), "\n")
	changed := false
	for i, l := range lines {
		// ponytail: an image line with a trailing comment or quotes is not matched, add when the installer writes one.
		m := imageLine.FindStringSubmatch(strings.TrimSuffix(l, "\r"))
		if m == nil || m[2] == image || docker.ImageRepo(m[2]) != repo {
			continue
		}
		lines[i] = m[1] + image + m[3]
		changed = true
	}
	if !changed {
		return false, nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	// Written beside the file and renamed over it: a crash must not leave half a stack file.
	tmp, err := os.CreateTemp(filepath.Dir(path), ".stack-*")
	if err != nil {
		return false, err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(strings.Join(lines, "\n")); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Chmod(info.Mode().Perm()); err != nil {
		tmp.Close()
		return false, err
	}
	if err := tmp.Close(); err != nil {
		return false, err
	}
	return true, os.Rename(tmp.Name(), path)
}
