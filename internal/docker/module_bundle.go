package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const moduleBundlePath = "/usr/share/modularnost/deploy"
const moduleBundleManifest = "manifest.json"

type ModuleBundle struct {
	StackFiles          []string
	Files               map[string][]byte
	RegistryCredentials bool
}

// ReadModuleBundle reads the deployment files without running image code.
func (c *Client) ReadModuleBundle(ctx context.Context, image string) (_ ModuleBundle, err error) {
	named, parseErr := reference.ParseNormalizedNamed(image)
	pinned, ok := named.(reference.Canonical)
	if parseErr != nil || !ok || pinned.Name()+"@"+pinned.Digest().String() != image || pinned.Digest().Algorithm() != "sha256" {
		return ModuleBundle{}, errors.New("module image must be pinned by sha256 digest")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	pull, err := c.api.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return ModuleBundle{}, fmt.Errorf("pull module image: %w", err)
	}
	if err := pull.Wait(ctx); err != nil {
		return ModuleBundle{}, fmt.Errorf("pull module image: %w", err)
	}

	name := "panel-module-bundle-" + rand.Text()
	created, createErr := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:       name,
		Config:     &container.Config{Image: image, Entrypoint: []string{"/bin/false"}},
		HostConfig: &container.HostConfig{NetworkMode: "none"},
	})
	if errdefs.IsConflict(createErr) {
		return ModuleBundle{}, fmt.Errorf("create module bundle container: %w", createErr)
	}
	// Docker may create the container before the client receives its ID.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, cleanupErr := c.api.ContainerRemove(cleanupCtx, name, client.ContainerRemoveOptions{RemoveVolumes: true})
		if cleanupErr != nil && !errdefs.IsNotFound(cleanupErr) {
			err = errors.Join(err, fmt.Errorf("remove module bundle container: %w", cleanupErr))
		}
	}()
	if createErr != nil {
		return ModuleBundle{}, fmt.Errorf("create module bundle container: %w", createErr)
	}
	result, err := c.api.CopyFromContainer(ctx, created.ID, client.CopyFromContainerOptions{SourcePath: moduleBundlePath})
	if err != nil {
		return ModuleBundle{}, fmt.Errorf("copy module bundle: %w", err)
	}
	defer result.Content.Close()
	return decodeModuleBundle(result.Content)
}

var bundlePath = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*(/[A-Za-z0-9][A-Za-z0-9._-]*)*$`)

func decodeModuleBundle(src io.Reader) (ModuleBundle, error) {
	files := make(map[string][]byte)
	limited := &io.LimitedReader{R: src, N: (1 << 20) + 1}
	archive := tar.NewReader(limited)
	for {
		header, err := archive.Next()
		if limited.N == 0 {
			return ModuleBundle{}, errors.New("module bundle exceeds size limit")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return ModuleBundle{}, fmt.Errorf("read module bundle archive: %w", err)
		}
		name, withinRoot := strings.CutPrefix(header.Name, "deploy/")
		if header.Typeflag == tar.TypeDir && (header.Name == "deploy" || withinRoot && (name == "" || bundlePath.MatchString(strings.TrimSuffix(name, "/")))) {
			continue
		}
		if _, duplicate := files[name]; !withinRoot || !bundlePath.MatchString(name) ||
			duplicate || header.Typeflag != tar.TypeReg || header.Size > 256<<10 || len(files) >= 64 {
			return ModuleBundle{}, fmt.Errorf("invalid module bundle file %q", header.Name)
		}
		files[name], err = io.ReadAll(archive)
		if err != nil {
			return ModuleBundle{}, fmt.Errorf("read module bundle file %q: %w", name, err)
		}
	}
	for name := range files {
		for parent := path.Dir(name); parent != "."; parent = path.Dir(parent) {
			if _, exists := files[parent]; exists {
				return ModuleBundle{}, fmt.Errorf("module bundle file %q is also used as a directory", parent)
			}
		}
	}
	return parseModuleBundleManifest(files)
}

func parseModuleBundleManifest(files map[string][]byte) (ModuleBundle, error) {
	raw, ok := files[moduleBundleManifest]
	if !ok {
		return ModuleBundle{}, errors.New("missing module bundle manifest.json")
	}
	var manifest struct {
		SchemaVersion       int      `json:"schema_version"`
		StackFiles          []string `json:"stack_files"`
		RegistryCredentials bool     `json:"registry_credentials"`
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&manifest); err != nil {
		return ModuleBundle{}, fmt.Errorf("invalid module bundle manifest: %w", err)
	}
	if decoder.Decode(new(any)) != io.EOF || manifest.SchemaVersion != 1 || len(manifest.StackFiles) == 0 {
		return ModuleBundle{}, errors.New("expected one version 1 module bundle manifest with stack files")
	}
	seen := make(map[string]bool, len(manifest.StackFiles))
	for _, name := range manifest.StackFiles {
		ext := path.Ext(name)
		if !bundlePath.MatchString(name) || (ext != ".yaml" && ext != ".yml") || seen[name] || len(files[name]) == 0 {
			return ModuleBundle{}, fmt.Errorf("invalid or missing module stack file %q", name)
		}
		seen[name] = true
	}
	delete(files, moduleBundleManifest)
	return ModuleBundle{StackFiles: manifest.StackFiles, Files: files, RegistryCredentials: manifest.RegistryCredentials}, nil
}
