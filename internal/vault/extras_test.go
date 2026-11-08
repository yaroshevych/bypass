package vault

import "testing"

func TestDatabaseCloneAndClearIncludeHistoryWithoutNesting(t *testing.T) {
	db := &Database{Entries: []Entry{{
		Fields:  []Field{{Name: "Title", Value: "current"}},
		History: []Entry{{Fields: []Field{{Name: "Title", Value: "snapshot"}, {Name: "Password", Value: "old-secret"}}, Tags: []string{"old"}, AttachmentNames: []string{"old.bin"}, History: []Entry{{Fields: []Field{{Name: "Title", Value: "nested"}}}}}},
	}}}
	clone := db.Clone()
	if len(clone.Entries[0].History) != 1 || len(clone.Entries[0].History[0].History) != 0 {
		t.Fatal("clone did not copy one level of history")
	}
	clone.Entries[0].History[0].Fields[0].Value = "changed"
	clone.Entries[0].History[0].Tags[0] = "changed"
	clone.Entries[0].History[0].AttachmentNames[0] = "changed.bin"
	if db.Entries[0].History[0].Get("Title") != "snapshot" || db.Entries[0].History[0].Tags[0] != "old" || db.Entries[0].History[0].AttachmentNames[0] != "old.bin" {
		t.Fatal("clone shares mutable snapshot slices with source database")
	}
	db.Clear()
	if len(db.Entries) != 0 || len(clone.Entries[0].History[0].Fields) != 2 {
		t.Fatal("clearing the source changed its clone or retained source entries")
	}
	if clone.Entries[0].History[0].Fields[1].Value != "old-secret" {
		t.Fatal("clone secret unexpectedly changed when source database cleared")
	}
}

func TestClearZerosCurrentAndSnapshotFieldReferences(t *testing.T) {
	db := &Database{Entries: []Entry{{Fields: []Field{{Name: "Password", Value: "current-secret"}}, History: []Entry{{Fields: []Field{{Name: "Password", Value: "snapshot-secret"}}}}}}}
	current := db.Entries[0].Fields
	history := db.Entries[0].History[0].Fields
	db.Clear()
	if current[0].Value != "" || history[0].Value != "" {
		t.Fatal("Clear did not drop current and historical secret references")
	}
}
