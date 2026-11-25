package docker

import (
	"archive/tar"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/containerd/errdefs"
	"github.com/distribution/reference"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

const builderBundlePath = "/usr/share/builder/deploy"

type BuilderBundle struct {
	Stack, RegistryPullStack                       []byte
	BuildKitStart, BuildKitNetwork, BuildKitConfig []byte
	RegistryPullConfig                             []byte
}

// ReadBuilderBundle reads the deployment files without running image code.
func (c *Client) ReadBuilderBundle(ctx context.Context, image string) (_ BuilderBundle, err error) {
	named, parseErr := reference.ParseNormalizedNamed(image)
	pinned, ok := named.(reference.Canonical)
	if parseErr != nil || !ok || pinned.Name()+"@"+pinned.Digest().String() != image || pinned.Digest().Algorithm() != "sha256" {
		return BuilderBundle{}, errors.New("image for Builder must be pinned by sha256 digest")
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	pull, err := c.api.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		return BuilderBundle{}, fmt.Errorf("pull Builder image: %w", err)
	}
	if err := pull.Wait(ctx); err != nil {
		return BuilderBundle{}, fmt.Errorf("pull Builder image: %w", err)
	}

	name := "panel-builder-bundle-" + rand.Text()
	created, createErr := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name:       name,
		Config:     &container.Config{Image: image, Entrypoint: []string{"/bin/false"}},
		HostConfig: &container.HostConfig{NetworkMode: "none"},
	})
	if errdefs.IsConflict(createErr) {
		return BuilderBundle{}, fmt.Errorf("create Builder bundle container: %w", createErr)
	}
	// Docker may create the container before the client receives its ID.
	defer func() {
		cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		_, cleanupErr := c.api.ContainerRemove(cleanupCtx, name, client.ContainerRemoveOptions{RemoveVolumes: true})
		if cleanupErr != nil && !errdefs.IsNotFound(cleanupErr) {
			err = errors.Join(err, fmt.Errorf("remove Builder bundle container: %w", cleanupErr))
		}
	}()
	if createErr != nil {
		return BuilderBundle{}, fmt.Errorf("create Builder bundle container: %w", createErr)
	}
	result, err := c.api.CopyFromContainer(ctx, created.ID, client.CopyFromContainerOptions{SourcePath: builderBundlePath})
	if err != nil {
		return BuilderBundle{}, fmt.Errorf("read Builder bundle: %w", err)
	}
	defer result.Content.Close()
	return decodeBuilderBundle(result.Content)
}

func decodeBuilderBundle(src io.Reader) (BuilderBundle, error) {
	var bundle BuilderBundle
	files := map[string]*[]byte{
		"deploy/stack.yml":                       &bundle.Stack,
		"deploy/stack.registry-pull.yml":         &bundle.RegistryPullStack,
		"deploy/scripts/buildkitd.sh":            &bundle.BuildKitStart,
		"deploy/scripts/buildkitd-network.sh":    &bundle.BuildKitNetwork,
		"deploy/scripts/buildkitd.toml":          &bundle.BuildKitConfig,
		"deploy/scripts/registry-pull.Caddyfile": &bundle.RegistryPullConfig,
	}
	limited := &io.LimitedReader{R: src, N: 512 << 10}
	archive := tar.NewReader(limited)
	for {
		header, err := archive.Next()
		if limited.N == 0 {
			return BuilderBundle{}, errors.New("deployment bundle for Builder exceeds size limit")
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return BuilderBundle{}, fmt.Errorf("read Builder bundle archive: %w", err)
		}
		file, known := files[header.Name]
		if !known {
			continue
		}
		if *file != nil || header.Typeflag != tar.TypeReg || header.Size <= 0 || header.Size > 64<<10 {
			return BuilderBundle{}, fmt.Errorf("invalid Builder bundle file %q", header.Name)
		}
		*file, err = io.ReadAll(archive)
		if err != nil {
			return BuilderBundle{}, fmt.Errorf("read Builder bundle file %q: %w", header.Name, err)
		}
	}
	for name, file := range files {
		if *file == nil {
			return BuilderBundle{}, fmt.Errorf("missing Builder bundle file %q", name)
		}
	}
	return bundle, nil
}
