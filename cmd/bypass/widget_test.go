package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"bypass/internal/vault"
)

func TestWidgetServiceLockedState(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var out bytes.Buffer
	if err := widgetCommandWithApprovals(strings.NewReader("{\"action\":\"recent\"}\n{\"action\":\"items\"}\n"), &out, false); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&out)
	var recent, items widgetResponse
	if err := decoder.Decode(&recent); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&items); err != nil {
		t.Fatal(err)
	}
	if recent.Action != "recent" || recent.Error != "" || items.Action != "items" || items.Error != "vault is locked" {
		t.Fatalf("unexpected responses: %#v %#v", recent, items)
	}
}

func TestWidgetServiceOpensAndSearchesVault(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	backend := vault.NewBackend()
	creds := vault.Credentials{Password: []byte("test master")}
	db, err := backend.CreateWithOptions(ctx, path, creds, vault.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := backend.Apply(ctx, db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Example", "Tags": "Favorite"}}, filepath.Join(t.TempDir(), "backups"))
	db.Clear()
	if err != nil {
		t.Fatal(err)
	}
	updated.Clear()
	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	if err := encoder.Encode(widgetRequest{Action: "open", Path: path, Password: "test master"}); err != nil {
		t.Fatal(err)
	}
	if err := encoder.Encode(widgetRequest{Action: "items", Query: "example"}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := widgetCommandWithApprovals(&input, &output, false); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(output.Bytes(), []byte("test master")) {
		t.Fatal("output contains password")
	}
	decoder := json.NewDecoder(&output)
	var opened, items widgetResponse
	if err := decoder.Decode(&opened); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&items); err != nil {
		t.Fatal(err)
	}
	if opened.Error != "" || items.Error != "" || len(items.Items) != 1 || items.Items[0].Title != "Example" {
		t.Fatalf("unexpected results: %#v %#v", opened, items)
	}
}
