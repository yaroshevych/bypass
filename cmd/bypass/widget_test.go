package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"bypass/internal/platform"
	"bypass/internal/vault"
)

func TestWidgetRecentCommand(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	paths, err := platform.Resolve()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	if err := platform.NewStore(paths).Remember(path); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := run([]string{"widget-recent"}, &out, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var response widgetResponse
	if err := json.NewDecoder(&out).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Action != "recent" || len(response.Recent) != 1 || response.Recent[0] != path {
		t.Fatalf("unexpected recent response: %#v", response)
	}
}

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

func TestWidgetServiceShutdownStopsProcessing(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var out bytes.Buffer
	input := strings.NewReader("{\"action\":\"shutdown\"}\n{\"action\":\"recent\"}\n")
	if err := widgetCommandWithApprovals(input, &out, false); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&out)
	var response widgetResponse
	if err := decoder.Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Action != "shutdown" || response.Error != "" {
		t.Fatalf("unexpected response: %#v", response)
	}
	if decoder.Decode(&response) != io.EOF {
		t.Fatal("service processed a request after shutdown")
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

func TestWidgetServiceRejectsInvalidMetadataConsent(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	var out bytes.Buffer
	if err := widgetCommandWithApprovals(strings.NewReader("{\"action\":\"add-favorite\",\"id\":\"entry\",\"consent\":\"invalid\"}\n"), &out, false); err != nil {
		t.Fatal(err)
	}
	var response widgetResponse
	if err := json.NewDecoder(&out).Decode(&response); err != nil {
		t.Fatal(err)
	}
	if response.Error != "invalid metadata loss consent" || response.ID != "entry" {
		t.Fatalf("unexpected response: %#v", response)
	}
}
