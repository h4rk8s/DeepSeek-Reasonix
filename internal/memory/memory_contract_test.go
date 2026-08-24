package memory

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMemoryOldFormatLoadsWithoutWritingMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "old-fact.md")
	old := "---\nname: old-fact\ntitle: Old Fact\ndescription: Existing note\ntype: project\n---\n\nbody\n"
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	store := Store{Dir: dir}
	memories := store.List()
	if len(memories) != 1 {
		t.Fatalf("List() returned %d memories, want 1", len(memories))
	}
	if memories[0].Name != "old-fact" || memories[0].Title != "Old Fact" || memories[0].Description != "Existing note" || string(memories[0].Type) != "project" || memories[0].Body != "body" {
		t.Fatalf("old memory parsed incorrectly: %+v", memories[0])
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("List() should not migrate/write old memory files on read\nbefore:\n%s\nafter:\n%s", before, after)
	}
}

func TestMemoryRecallAllowsDuplicateHitsToFillLimitToday(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"cache-prefix-a", "cache-prefix-b", "cache-prefix-c"} {
		body := "---\nname: " + name + "\ntitle: Cache Prefix\ndescription: cache prefix hit rate tuning\ntype: project\n---\n\ncache prefix hit rate tuning repeated note\n"
		if err := os.WriteFile(filepath.Join(dir, name+".md"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	hits, err := searchMemories(context.Background(), Store{Dir: dir}, "cache prefix hit rate", "", 3)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, hit := range hits {
		names = append(names, hit.Memory.Name)
	}
	want := []string{"cache-prefix-a", "cache-prefix-b", "cache-prefix-c"}
	if !reflect.DeepEqual(names, want) {
		t.Fatalf("duplicate recall baseline changed: got %v want %v", names, want)
	}
}
