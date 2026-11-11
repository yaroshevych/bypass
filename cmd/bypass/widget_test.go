package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
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
