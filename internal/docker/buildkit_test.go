package docker

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/api/types/swarm"
	"github.com/moby/moby/client"
)

func makeBuildKitTLS(t *testing.T) (BuildKitTLS, string, string) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	root := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "Builder test CA"}, NotBefore: now.Add(-time.Hour), NotAfter: now.Add(time.Hour),
		IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	ca, err := x509.CreateCertificate(rand.Reader, root, root, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	issue := func(serial int64, names []string, usage x509.ExtKeyUsage) (string, string) {
		p, k, err := ed25519.GenerateKey(rand.Reader)
		if err != nil {
			t.Fatal(err)
		}
		cert, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{SerialNumber: big.NewInt(serial), DNSNames: names, NotBefore: root.NotBefore, NotAfter: root.NotAfter,
			KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{usage}}, root, p, key)
		if err != nil {
			t.Fatal(err)
		}
		raw, err := x509.MarshalPKCS8PrivateKey(k)
		if err != nil {
			t.Fatal(err)
		}
		return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: cert})), string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: raw}))
	}
	server, serverKey := issue(2, []string{"buildkitd"}, x509.ExtKeyUsageServerAuth)
	cl, clKey := issue(3, nil, x509.ExtKeyUsageClientAuth)
	registryCert, registryKey := issue(4, []string{"registry"}, x509.ExtKeyUsageServerAuth)
	rootPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca}))
	return BuildKitTLS{CA: rootPEM, ServerCertificate: server, ServerKey: serverKey, RegistryCA: rootPEM, RegistryClientCert: cl, RegistryClientKey: clKey}, registryCert, registryKey
}

func TestBuildKitTLS(t *testing.T) {
	in, _, _ := makeBuildKitTLS(t)
	if err := in.Validate(); err != nil {
		t.Fatal(err)
	}
	if _, err := buildKitFiles(in); err != nil {
		t.Fatal(err)
	}
	in.RegistryClientCert, in.RegistryClientKey = in.ServerCertificate, in.ServerKey
	if err := in.Validate(); err == nil {
		t.Fatal("accepted a server-only certificate as a registry client")
	}
}

func TestProvisionBuildKitOnSwarm(t *testing.T) {
	if os.Getenv("PANEL_SWARM_BUILDKIT") != "1" {
		t.Skip("set PANEL_SWARM_BUILDKIT=1 on a disposable Swarm manager")
	}
	c, err := New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	ctx := t.Context()
	overlay := "builder-fixture-" + rand.Text()
	id, err := c.CreateNetwork(ctx, NewNetwork{Name: overlay, Internal: true, Attachable: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.RemoveNetwork(context.Background(), id) })
	creds, registryCert, registryKey := makeBuildKitTLS(t)
	startBuildKitTestRegistry(t, c, overlay, creds.RegistryCA, registryCert, registryKey)
	worker, err := c.ProvisionBuildKit(ctx, overlay, creds)
	if err != nil {
		t.Fatal(err)
	}
	if worker == "" {
		t.Fatal("test requires a disposable daemon without an existing managed BuildKit container")
	}
	defer func() {
		_ = c.RemoveBuildKit(context.Background(), worker)
		_ = c.RemoveLocalVolume(context.Background(), buildKitName)
	}()
	if id, err := c.ProvisionBuildKit(ctx, overlay, creds); err != nil || id != "" {
		t.Fatalf("retry replaced BuildKit: %q %v", id, err)
	}
	verifyBuilderFixtureClient(t, c, id, creds)
	probe, err := c.SeedBuilderProbe(ctx, "registry.example.test")
	if err != nil || !ValidRegistryProbe(probe, "registry.example.test") {
		t.Fatalf("publish fixture through mTLS: %s %v", probe, err)
	}
}

func startBuildKitTestRegistry(t *testing.T, c *Client, overlay, ca, cert, key string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	const image = "registry:3@sha256:82f6ff4a7f982960c8aa51617fea3aabc485df6e55cce80d22deb1de36f1bc3c"
	pull, err := c.api.ImagePull(ctx, image, client.ImagePullOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer pull.Close()
	if err := pull.Wait(ctx); err != nil {
		t.Fatal(err)
	}
	created, err := c.api.ContainerCreate(ctx, client.ContainerCreateOptions{
		Name: "builder-fixture-registry-" + rand.Text(),
		Config: &container.Config{Image: image, Env: []string{
			"REGISTRY_HTTP_TLS_CERTIFICATE=/server.pem", "REGISTRY_HTTP_TLS_KEY=/server-key.pem",
			"REGISTRY_HTTP_TLS_CLIENTCAS=[/ca.pem]", "REGISTRY_HTTP_TLS_CLIENTAUTH=require-and-verify-client-cert",
		}},
		HostConfig:       &container.HostConfig{NetworkMode: container.NetworkMode(overlay)},
		NetworkingConfig: &network.NetworkingConfig{EndpointsConfig: map[string]*network.EndpointSettings{overlay: {Aliases: []string{"registry"}}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = c.api.ContainerRemove(context.Background(), created.ID, client.ContainerRemoveOptions{Force: true, RemoveVolumes: true})
	})
	var archive bytes.Buffer
	writer := tar.NewWriter(&archive)
	for name, data := range map[string]string{"ca.pem": ca, "server.pem": cert, "server-key.pem": key} {
		if err := writer.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data))}); err != nil {
			t.Fatal(err)
		}
		if _, err := writer.Write([]byte(data)); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := c.api.CopyToContainer(ctx, created.ID, client.CopyToContainerOptions{DestinationPath: "/", Content: &archive}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.api.ContainerStart(ctx, created.ID, client.ContainerStartOptions{}); err != nil {
		t.Fatal(err)
	}
}

func verifyBuilderFixtureClient(t *testing.T, c *Client, overlay string, creds BuildKitTLS) {
	t.Helper()
	ctx := t.Context()
	inputs := map[string]string{"builder_buildkit_ca": creds.CA, "builder_buildkit_client": creds.RegistryClientCert, "builder_buildkit_client_key": creds.RegistryClientKey}
	if err := c.EnsureBuilderSecrets(ctx, inputs); err != nil {
		t.Fatal(err)
	}
	if err := c.EnsureBuilderSecrets(ctx, inputs); err != nil {
		t.Fatalf("reusing uploaded secrets: %v", err)
	}
	refs, err := c.BuilderSecrets(ctx)
	if err != nil {
		t.Fatal(err)
	}
	name := "builder-fixture-module-" + rand.Text()
	one := uint64(1)
	service, err := c.api.ServiceCreate(ctx, client.ServiceCreateOptions{Spec: swarm.ServiceSpec{
		Annotations: swarm.Annotations{Name: name},
		TaskTemplate: swarm.TaskSpec{
			ContainerSpec: &swarm.ContainerSpec{Image: buildKitImage, Command: []string{"/bin/sh", "-c", "sleep 300"}, User: "10001:10001", Secrets: refs},
			Networks:      []swarm.NetworkAttachmentConfig{{Target: overlay}},
		},
		Mode: swarm.ServiceMode{Replicated: &swarm.ReplicatedService{Replicas: &one}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = c.RemoveService(context.Background(), service.ID)
		for _, ref := range refs {
			_ = c.RemoveSecret(context.Background(), ref.SecretID)
		}
	}()
	if err := c.VerifyBuilderConnection(ctx, name); err != nil {
		t.Fatal(err)
	}
}
