package store

import "testing"

func TestModuleVolumeKeepsOriginalNodeAndName(t *testing.T) {
	db, err := Open(t.TempDir() + "/module.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := t.Context()
	if _, err := db.ModuleStorageNode(ctx, "metrics", "original"); err != nil {
		t.Fatal(err)
	}
	node, volume, err := db.ModuleVolume(ctx, "metrics", "replacement", "saved")
	if err != nil || node != "original" || volume != "saved" {
		t.Fatalf("legacy selection = (%s,%s,%v)", node, volume, err)
	}
	node, volume, err = db.ModuleVolume(ctx, "metrics", "replacement", "empty")
	if err != nil || node != "original" || volume != "saved" {
		t.Fatalf("repeat selection = (%s,%s,%v)", node, volume, err)
	}
}
