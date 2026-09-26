package db

import "testing"

// TestCheckDuplicateVersions_Duplicate confirms two migrations claiming the
// same version number are rejected with a clear error rather than silently
// letting both exist -- this is what would have let this branch's
// 0007_device_platform collide with an unrelated PR's own 0007 and only have
// one of the two actually applied.
func TestCheckDuplicateVersions_Duplicate(t *testing.T) {
	migrations := []migration{
		{version: 1, name: "0001_init.up.sql"},
		{version: 7, name: "0007_device_platform.up.sql"},
		{version: 7, name: "0007_blocklist_keyset_index.up.sql"},
	}

	err := checkDuplicateVersions(migrations)
	if err == nil {
		t.Fatal("checkDuplicateVersions: want error for duplicate version 7, got nil")
	}
}

// TestCheckDuplicateVersions_NoDuplicates confirms a normal, distinct set of
// migration versions passes cleanly.
func TestCheckDuplicateVersions_NoDuplicates(t *testing.T) {
	migrations := []migration{
		{version: 1, name: "0001_init.up.sql"},
		{version: 2, name: "0002_drop_duplicate_number_index.up.sql"},
		{version: 11, name: "0011_device_platform.up.sql"},
	}

	if err := checkDuplicateVersions(migrations); err != nil {
		t.Fatalf("checkDuplicateVersions: want nil for distinct versions, got %v", err)
	}
}
