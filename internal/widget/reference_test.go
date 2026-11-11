package widget

import (
	"reflect"
	"testing"

	"bypass/internal/run"
)

func TestFieldReferenceRoundTrip(t *testing.T) {
	got, err := FieldReference("/home/user/my vault.kdbx", "Passwords/Work/Cloud", "Item #1", "API/Key")
	if err != nil {
		t.Fatal(err)
	}
	ref, err := run.ParseReference(got)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"home", "user", "my vault.kdbx", "Work", "Cloud", "Item #1"}
	if !ref.Absolute || !reflect.DeepEqual(ref.Components, want) || ref.Field != "API/Key" {
		t.Fatalf("incorrect reference: %#v", ref)
	}
}

func TestFieldReferenceRejectsInvalidInput(t *testing.T) {
	for _, input := range []struct{ path, title, field string }{{"relative.kdbx", "item", "field"}, {"/vault.kdbx", "", "field"}, {"/vault.kdbx", "item", ""}, {"/vault.kdbx", "item", "bad\x00field"}} {
		if _, err := FieldReference(input.path, "", input.title, input.field); err == nil {
			t.Fatal("accepted invalid input")
		}
	}
}

func TestRelativeFieldReferenceMatchesWidgetPath(t *testing.T) {
	got, err := relativeFieldReference("", "Amazon", "Password")
	if err != nil || got != "bypass://Amazon#Password" {
		t.Fatalf("got %q, err %v", got, err)
	}
}
