package main

import "testing"

func TestValidateAlias(t *testing.T) {
	if got, err := validateAlias("  billing  "); err != nil || got != "billing" {
		t.Errorf("trim: got %q, %v", got, err)
	}
	if got, err := validateAlias("   "); err != nil || got != "" {
		t.Errorf("blank should mean default: got %q, %v", got, err)
	}
	if _, err := validateAlias("bad\nname"); err == nil {
		t.Error("control characters should be rejected")
	}
	long := make([]rune, maxAliasLen+1)
	for i := range long {
		long[i] = 'x'
	}
	if _, err := validateAlias(string(long)); err == nil {
		t.Error("over-long alias should be rejected")
	}
}

func TestNextAliasFillsLowestGap(t *testing.T) {
	keys := []*Key{{Label: "key 1"}, {Label: "key 3"}}
	if got := nextAlias(keys); got != "key 2" {
		t.Errorf("nextAlias = %q, want key 2", got)
	}
	if got := nextAlias(nil); got != "key 1" {
		t.Errorf("nextAlias(empty) = %q, want key 1", got)
	}
}

func TestAliasesAreUniqueCaseInsensitively(t *testing.T) {
	keys := []*Key{{ID: "a", Label: "Billing"}}
	if !aliasInUse(keys, "billing", nil) {
		t.Error("billing should clash with Billing")
	}
	if aliasInUse(keys, "billing", keys[0]) {
		t.Error("a key may keep its own alias")
	}
}

func TestAssignAliasesKeepsExistingLabels(t *testing.T) {
	keys := []*Key{
		{ID: "1"},
		{ID: "2", Label: "billing"},
		{ID: "3", Label: "sk-p...CCCC"}, // masked label from an older version
		{ID: "4"},
	}
	assignAliases(keys)
	want := []string{"key 1", "billing", "sk-p...CCCC", "key 2"}
	for i, k := range keys {
		if k.Label != want[i] {
			t.Errorf("key %d label = %q, want %q", i, k.Label, want[i])
		}
	}
}

func TestAddKeyDefaultsAndRejectsDuplicates(t *testing.T) {
	p := &Provider{ID: "p"}
	first := &Key{ID: "a", Value: "v1"}
	if err := p.addKey(first, ""); err != nil || first.Label != "key 1" {
		t.Fatalf("default alias: label=%q err=%v", first.Label, err)
	}
	if err := p.addKey(&Key{ID: "b", Value: "v2"}, "KEY 1"); err != errAliasTaken {
		t.Errorf("duplicate alias err = %v, want errAliasTaken", err)
	}
	if err := p.addKey(&Key{ID: "c", Value: "v3"}, "prod"); err != nil {
		t.Fatal(err)
	}
	if len(p.Keys) != 2 {
		t.Errorf("keys = %d, want 2 (rejected key must not be added)", len(p.Keys))
	}
}

func TestRenameKeyChecksConflicts(t *testing.T) {
	p := &Provider{ID: "p", Keys: []*Key{{ID: "a", Label: "key 1"}, {ID: "b", Label: "key 2"}}}
	if found, err := p.renameKey("a", "key 2"); !found || err != errAliasTaken {
		t.Errorf("rename onto another key: found=%v err=%v", found, err)
	}
	if found, err := p.renameKey("a", "key 1"); !found || err != nil {
		t.Errorf("renaming to its own alias should succeed: found=%v err=%v", found, err)
	}
	if found, _ := p.renameKey("zzz", "x"); found {
		t.Error("unknown key id should report not found")
	}
	if _, err := p.renameKey("a", ""); err == nil {
		t.Error("empty alias should be rejected on rename")
	}
	if found, err := p.renameKey("a", "billing"); !found || err != nil || p.Keys[0].Label != "billing" {
		t.Errorf("rename: found=%v err=%v label=%q", found, err, p.Keys[0].Label)
	}
}
