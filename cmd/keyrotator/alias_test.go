package main

import "testing"

func TestNextAliasFillsLowestGap(t *testing.T) {
	if got := nextAlias([]*Key{{Label: "key 1"}, {Label: "key 3"}}); got != "key 2" {
		t.Errorf("nextAlias = %q, want key 2", got)
	}
}

// Older versions labelled keys with their masked value. Loading such a config
// must keep those labels and only name keys that have none.
func TestAssignAliasesKeepsExistingLabels(t *testing.T) {
	keys := []*Key{{ID: "1"}, {ID: "2", Label: "billing"}, {ID: "3", Label: "sk-p...CCCC"}, {ID: "4"}}
	assignAliases(keys)
	want := []string{"key 1", "billing", "sk-p...CCCC", "key 2"}
	for i, k := range keys {
		if k.Label != want[i] {
			t.Errorf("key %d label = %q, want %q", i, k.Label, want[i])
		}
	}
}

func TestAliasesStayUniqueOnAddAndRename(t *testing.T) {
	p := &Provider{ID: "p"}
	first := &Key{ID: "a"}
	if err := p.addKey(first, ""); err != nil || first.Label != "key 1" {
		t.Fatalf("default alias: label=%q err=%v", first.Label, err)
	}
	if err := p.addKey(&Key{ID: "b"}, "KEY 1"); err != errAliasTaken {
		t.Errorf("duplicate alias (other case) err = %v, want errAliasTaken", err)
	}
	if err := p.addKey(&Key{ID: "c"}, "prod"); err != nil {
		t.Fatal(err)
	}
	if found, err := p.renameKey("a", "Prod"); !found || err != errAliasTaken {
		t.Errorf("rename onto another key: found=%v err=%v", found, err)
	}
	if found, err := p.renameKey("a", "key 1"); !found || err != nil {
		t.Errorf("keeping its own alias should succeed: found=%v err=%v", found, err)
	}
	if len(p.Keys) != 2 {
		t.Errorf("keys = %d, want 2 (the rejected key must not be added)", len(p.Keys))
	}
}
