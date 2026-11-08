package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"bypass/internal/vault"
	"bypass/internal/vaulttest"
)

const (
	master   = "visual-demo-passphrase"
	password = "Visual-Demo-Only-42!"
)

type record struct{ group, title, user, url, tags, notes string }

var records = []record{
	{"Personal", "Proton Mail", "alex@example.test", "https://mail.proton.me", "Favorite;personal", "Personal email account.\nRecovery codes in this fixture are examples only."},
	{"Personal", "Netflix", "alex@example.test", "https://netflix.com", "personal", "Family streaming account."},
	{"Personal", "Spotify", "alex-listens", "https://spotify.com", "personal", ""},
	{"Personal", "Bank Account", "alex.s", "https://bank.example.test", "finance", "Monthly statements and payments."},
	{"Personal", "Home Wi-Fi", "home-network", "", "home", "Guest access uses a separate password."},
	{"Work", "GitHub", "alex-dev", "https://github.com", "Favorite;work", "Primary development account."},
	{"Work", "Linear", "alex@work.example", "https://linear.app", "work", ""},
	{"Work", "Figma", "alex@work.example", "https://figma.com", "work", ""},
	{"Work", "Cloud Console", "alex-admin", "https://console.example.test", "work", "Use the least-privileged role."},
	{"Work", "Package Registry", "alex-packages", "https://packages.example.test", "work", ""},
}

func main() {
	path := os.Getenv("BYPASS_SCREENSHOT_FIXTURE")
	if path == "" {
		fail("BYPASS_SCREENSHOT_FIXTURE is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		fail("create fixture directory: %v", err)
	}
	ctx := context.Background()
	creds := vault.Credentials{Password: []byte(master)}
	defer creds.Clear()
	backend := vaulttest.NewBackend()
	db, err := backend.Create(ctx, path, creds)
	check("create vault: %v", err)
	defer db.Clear()
	for _, name := range []string{"Personal", "Work", "Archive"} {
		db = apply(backend, ctx, db, creds, vault.Change{Kind: vault.CreateGroup, GroupUUID: db.RootUUID, NewName: name})
	}
	for _, r := range records {
		group := groupID(db, r.group)
		values := map[string]string{"Title": r.title, "UserName": r.user, "URL": r.url, "Notes": r.notes, "Tags": r.tags}
		if r.title == "Proton Mail" {
			values["otp"] = "otpauth://totp/VisualDemo?secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ&period=30"
		}
		db = apply(backend, ctx, db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: group, Values: values, PasswordChanged: true, Password: []byte(password)})
	}
	proton := entryID(db, "Proton Mail")
	db = apply(backend, ctx, db, creds, vault.Change{Kind: vault.EditEntry, EntryUUID: proton, Values: map[string]string{"Notes": "Earlier visual fixture notes."}})
	attachment := filepath.Join(filepath.Dir(path), "recovery-notes.txt")
	if err := os.WriteFile(attachment, []byte("Synthetic attachment for visual review. No real recovery codes.\n"), 0600); err != nil {
		fail("write attachment: %v", err)
	}
	data, err := os.ReadFile(attachment)
	check("read attachment: %v", err)
	db = apply(backend, ctx, db, creds, vault.Change{Kind: vault.AddAttachment, EntryUUID: proton, AttachmentName: filepath.Base(attachment), AttachmentData: data})
	db.Clear()
}

func apply(backend *vaulttest.Backend, ctx context.Context, db *vault.Database, creds vault.Credentials, change vault.Change) *vault.Database {
	updated, err := backend.Apply(ctx, db, creds, change, filepath.Join(filepath.Dir(db.Path), "backups"))
	db.Clear()
	if err != nil {
		fail("apply fixture change %q: %v", change.Kind, err)
	}
	return updated
}

func groupID(db *vault.Database, name string) string {
	for _, group := range db.Groups {
		if group.Name == name {
			return group.UUID
		}
	}
	fail("group %q was not found", name)
	return ""
}

func entryID(db *vault.Database, title string) string {
	for _, entry := range db.Entries {
		if entry.Get("Title") == title {
			return entry.UUID
		}
	}
	fail("entry %q was not found", title)
	return ""
}

func check(format string, err error) {
	if err != nil {
		fail(format, err)
	}
}
func fail(format string, args ...any) { fmt.Fprintf(os.Stderr, format+"\n", args...); os.Exit(1) }
