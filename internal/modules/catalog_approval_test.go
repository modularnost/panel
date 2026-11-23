package modules

import "testing"

func TestCatalogApprovalBindsManifestAndToken(t *testing.T) {
	entries, err := decodeCatalog([]byte(catalogFixture))
	if err != nil {
		t.Fatal(err)
	}
	entry := entries[0]
	module := Module{
		Name: entry.ID, serviceID: "service-one",
		Manifest: Manifest{Contract: ContractVersion, Name: entry.ID, Version: entry.Install.Version, Permissions: []string{"stats:read"}},
	}
	if err := module.loadApproval(entry.ApprovalLabel(), "different-image"); err == nil {
		t.Fatal("accepted a different image")
	}
	if err := module.loadApproval(entry.ApprovalLabel(), entry.Install.Image); err != nil {
		t.Fatal(err)
	}
	if err := module.verifyApprovedManifest(); err != nil {
		t.Fatal(err)
	}

	reg := NewRegistry(nil, "test-secret")
	reg.Pin(module)
	token := reg.moduleToken(module)
	if _, ok := reg.Authenticate(t.Context(), module.Name, token); !ok {
		t.Fatal("denied the installed service's token")
	}
	if _, ok := reg.Authenticate(t.Context(), module.Name, reg.Token(module.Name)); ok {
		t.Fatal("accepted a manual module token for a catalog module")
	}
	reinstalled := module
	reinstalled.serviceID = "service-two"
	reg.Pin(reinstalled)
	if _, ok := reg.Authenticate(t.Context(), module.Name, token); ok {
		t.Fatal("accepted a removed service's token")
	}

	module.Manifest.Permissions = []string{"stats:read", "service:create"}
	if err := module.verifyApprovedManifest(); err == nil {
		t.Fatal("accepted extra manifest permissions")
	}
	module.Err = "manifest permissions differ"
	reg.Pin(module)
	if _, ok := reg.Authenticate(t.Context(), module.Name, token); ok {
		t.Fatal("granted permissions to a module with an invalid manifest")
	}
}
