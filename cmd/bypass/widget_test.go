package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"bypass/internal/clipboard"
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
	if err := encoder.Encode(widgetRequest{Action: "items", Query: "foo:bar"}); err != nil {
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
	var opened, items, invalid widgetResponse
	if err := decoder.Decode(&opened); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&items); err != nil {
		t.Fatal(err)
	}
	if err := decoder.Decode(&invalid); err != nil {
		t.Fatal(err)
	}
	if opened.Error != "" || items.Error != "" || len(items.Items) != 1 || items.Items[0].Title != "Example" {
		t.Fatalf("unexpected results: %#v %#v", opened, items)
	}
	if invalid.Action != "items" || invalid.Error != "Invalid search. Check fields and quotes." || len(invalid.Items) != 0 {
		t.Fatalf("invalid search did not return actionable feedback: %#v", invalid)
	}
}

func TestWidgetReportsSensitiveClipboardAndRejectsSecretCopyWhenUnsupported(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Wayland capability test")
	}
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "wl-copy"), []byte("#!/bin/sh\nif [ \"$1\" = --help ]; then echo 'Usage: wl-copy'; exit 0; fi\nexec /bin/cat >/dev/null\n"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "wl-paste"), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)

	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	backend := vault.NewBackend()
	credentials := vault.Credentials{Password: []byte("synthetic widget master")}
	defer credentials.Clear()
	db, err := backend.CreateWithOptions(ctx, path, credentials, vault.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := backend.Apply(ctx, db, credentials, vault.Change{
		Kind: vault.AddEntry, GroupUUID: db.RootUUID,
		Values:          map[string]string{"Title": "Sensitive item"},
		PasswordChanged: true, Password: []byte("synthetic widget secret"),
	}, filepath.Join(t.TempDir(), "backups"))
	db.Clear()
	if err != nil {
		t.Fatal(err)
	}
	entryID := updated.Entries[0].UUID
	updated.Clear()

	var input bytes.Buffer
	encoder := json.NewEncoder(&input)
	for _, request := range []widgetRequest{
		{Action: "recent"},
		{Action: "open", Path: path, Password: string(credentials.Password)},
		{Action: "items", Query: "sensitive"},
		{Action: "copy", ID: entryID, Field: "Password"},
	} {
		if err := encoder.Encode(request); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := widgetCommandWithApprovals(&input, &output, false); err != nil {
		t.Fatal(err)
	}
	decoder := json.NewDecoder(&output)
	var recent, opened, items, copied widgetResponse
	for _, response := range []*widgetResponse{&recent, &opened, &items, &copied} {
		if err := decoder.Decode(response); err != nil {
			t.Fatal(err)
		}
	}
	if recent.SensitiveClipboard {
		t.Fatal("old wl-copy was advertised as sensitive-capable")
	}
	if opened.Error != "" || items.Error != "" || len(items.Items) != 1 {
		t.Fatalf("unsupported sensitive clipboard prevented browsing: open=%#v items=%#v", opened, items)
	}
	if !strings.Contains(copied.Error, clipboard.ErrSensitiveUnavailable.Error()) {
		t.Fatalf("secret copy error = %q, want actionable secure clipboard guidance", copied.Error)
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

func TestWidgetServiceNotifiesAndRefreshesAfterExternalEdit(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_STATE_HOME", t.TempDir())
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "vault.kdbx")
	backend := vault.NewBackend()
	creds := vault.Credentials{Password: []byte("synthetic master")}
	defer creds.Clear()
	db, err := backend.CreateWithOptions(ctx, path, creds, vault.CreateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { db.Clear() }()
	inReader, inWriter := io.Pipe()
	outReader, outWriter := io.Pipe()
	defer inWriter.Close()
	defer outReader.Close()
	done := make(chan error, 1)
	go func() {
		err := widgetCommandWithApprovals(inReader, outWriter, false)
		_ = outWriter.Close()
		done <- err
	}()
	responses := make(chan widgetResponse, 8)
	go func() {
		d := json.NewDecoder(outReader)
		for {
			var r widgetResponse
			if d.Decode(&r) != nil {
				return
			}
			responses <- r
		}
	}()
	send := json.NewEncoder(inWriter)
	if err := send.Encode(widgetRequest{Action: "open", Path: path, Password: "synthetic master"}); err != nil {
		t.Fatal(err)
	}
	read := func(action string) widgetResponse {
		t.Helper()
		select {
		case r := <-responses:
			if r.Action != action || r.Error != "" {
				t.Fatalf("response to %s: %#v", action, r)
			}
			return r
		case <-time.After(10 * time.Second):
			t.Fatalf("timed out waiting for %s", action)
			return widgetResponse{}
		}
	}
	read("open")
	updated, err := backend.Apply(ctx, db, creds, vault.Change{Kind: vault.AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Reloaded entry"}}, filepath.Join(t.TempDir(), "backups"))
	if err != nil {
		t.Fatal(err)
	}
	db.Clear()
	db = updated
	read("reloaded")
	if err := send.Encode(widgetRequest{Action: "items", Query: "Reloaded entry"}); err != nil {
		t.Fatal(err)
	}
	if r := read("items"); len(r.Items) != 1 || r.Items[0].Title != "Reloaded entry" {
		t.Fatalf("updated items: %#v", r.Items)
	}
	if err := send.Encode(widgetRequest{Action: "shutdown"}); err != nil {
		t.Fatal(err)
	}
	read("shutdown")
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
