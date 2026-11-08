package vault

import (
	"context"
	"errors"
	"testing"
)

func TestDatabaseAndCredentialsCloneAreIndependent(t *testing.T) {
	db := &Database{Groups: []Group{{Name: "group"}}, Entries: []Entry{{Fields: []Field{{Name: "Password", Value: "secret"}}, Tags: []string{"tag"}, AttachmentNames: []string{"file"}}}}
	clone := db.Clone()
	clone.Groups[0].Name = "changed"
	clone.Entries[0].Fields[0].Value = "changed"
	clone.Entries[0].Tags[0] = "changed"
	clone.Entries[0].AttachmentNames[0] = "changed"
	if db.Groups[0].Name != "group" || db.Entries[0].Fields[0].Value != "secret" || db.Entries[0].Tags[0] != "tag" || db.Entries[0].AttachmentNames[0] != "file" {
		t.Fatal("database clone shares mutable slices")
	}
	creds := Credentials{Password: []byte("secret"), KeyFile: "key", YubiKey: "slot"}
	copy := creds.Clone()
	copy.Password[0] = 'x'
	copy.Clear()
	if string(creds.Password) != "secret" || len(copy.Password) != 0 || copy.KeyFile != "" || copy.YubiKey != "" {
		t.Fatal("credential clone/clear did not isolate the source")
	}
}

func TestValidateChangeRejectsUnsupportedFieldsAndPayloads(t *testing.T) {
	cases := []Change{
		{Kind: ChangeKind("unknown")},
		{Kind: AddEntry, Values: map[string]string{"PrivateUnsupportedField": "value"}},
		{Kind: AddEntry, Password: []byte("password"), PasswordChanged: false},
		{Kind: AddEntry, AttachmentName: "orphaned.bin"},
		{Kind: AddAttachment, EntryUUID: "id", AttachmentName: "bad\nname"},
		{Kind: AddAttachment, EntryUUID: "id", AttachmentName: "file", AttachmentData: make([]byte, maxAttachmentBytes+1)},
	}
	for _, change := range cases {
		if err := validateChange(change); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("unsupported mutation was accepted, error present=%t", err != nil)
		}
	}
	if err := validateChange(Change{Kind: AddEntry, Values: map[string]string{"Title": "Entry", "Notes": "safe"}, Password: []byte("synthetic password"), PasswordChanged: true}); err != nil {
		t.Fatalf("supported mutation rejected: %v", err)
	}
}

func TestNativeGenerateBoundary(t *testing.T) {
	backend := NewBackend()
	got, err := backend.Generate(context.Background(), 8)
	if err != nil || len(got) != 8 {
		t.Fatalf("Generate returned %d bytes, error present=%t", len(got), err != nil)
	}
	zero(got)
	for _, length := range []int{0, maxGeneratedPasswordLength + 1} {
		if _, err := backend.Generate(context.Background(), length); !errors.Is(err, ErrUnsupported) {
			t.Fatalf("invalid generator length %d was accepted", length)
		}
	}
}
