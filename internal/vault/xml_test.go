package vault

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"
)

func uuidBytes(b byte) string {
	raw := make([]byte, 16)
	for i := range raw {
		raw[i] = b
	}
	return base64.StdEncoding.EncodeToString(raw)
}

func fixtureXML() string {
	root, ordinary, bin := uuidBytes(1), uuidBytes(2), uuidBytes(4)
	return fmt.Sprintf(`<?xml version="1.0"?><KeePassFile><Meta><DatabaseName>Demo</DatabaseName><RecycleBinEnabled>True</RecycleBinEnabled><RecycleBinUUID>%s</RecycleBinUUID></Meta><Root><Group><UUID>%s</UUID><Name>Root</Name><EnableSearching>True</EnableSearching><Group><UUID>%s</UUID><Name>Empty &amp; nested</Name><EnableSearching>False</EnableSearching></Group><Group><UUID>%s</UUID><Name>Trash</Name><Group><UUID>%s</UUID><Name>Subtrash</Name><Entry><UUID>%s</UUID><Tags>one two</Tags><Times><CreationTime>2024-01-02T03:04:05Z</CreationTime><LastModificationTime>AAgAAAAAAAA=</LastModificationTime><ExpiryTime>2024-01-03T03:04:05Z</ExpiryTime><Expires>True</Expires></Times><String><Key>Title</Key><Value>Sample</Value></String><String><Key>Password</Key><Value>secret</Value></String><String><Key>Token</Key><Value Protected="True">secret-token</Value></String><String><Key>Notes</Key><Value ProtectInMemory="True"></Value></String><Binary><Key>file.txt</Key><Value Ref="0"/></Binary><History><Entry/><Entry/></History></Entry></Group></Group></Group></Root></KeePassFile>`, uuidBytes(4), root, ordinary, bin, uuidBytes(5), uuidBytes(6))
}

func TestParseXMLPreservesStructureAndMetadata(t *testing.T) {
	data := strings.NewReplacer(
		"<DatabaseName>Demo</DatabaseName>", "<DatabaseName> Demo </DatabaseName>",
		"<Name>Empty &amp; nested</Name>", "<Name> Empty &amp; nested </Name>",
		"<Key>Title</Key>", "<Key> Title </Key>",
		"<Tags>one two</Tags>", "<Tags> Favorite;work tag, Favorite, , </Tags>",
		"<Key>file.txt</Key>", "<Key> file.txt </Key>",
	).Replace(fixtureXML())
	db, err := parseXML([]byte(data), "test.kdbx")
	if err != nil {
		t.Fatal(err)
	}
	if db.Name != " Demo " || !db.RecycleBinEnabled || len(db.Groups) != 4 || len(db.Entries) != 1 {
		t.Fatalf("unexpected database: %+v", db)
	}
	if db.RootUUID != "01010101-0101-0101-0101-010101010101" || db.RecycleBinUUID != "04040404-0404-0404-0404-040404040404" {
		t.Fatalf("UUID normalization failed: %#v", db)
	}
	if db.Groups[1].Path != "Root/ Empty & nested " || db.Groups[1].Name != " Empty & nested " || db.Groups[1].ParentUUID != db.RootUUID || db.Groups[1].SearchEnabled {
		t.Fatalf("empty group/inheritance lost: %+v", db.Groups[1])
	}
	if !db.Groups[2].SearchEnabled || !db.Groups[3].Recycled {
		t.Fatalf("search or recycle inheritance wrong: %+v", db.Groups)
	}
	e := db.Entries[0]
	if !e.Recycled || e.GroupPath != "Root/Trash/Subtrash" || e.HistoryCount != 2 || len(e.AttachmentNames) != 1 || e.AttachmentNames[0] != " file.txt " {
		t.Fatalf("entry metadata lost: %+v", e)
	}
	if e.Get("Password") != "secret" || e.Get(" Title ") != "Sample" || !e.IsProtected("Password") || !e.IsProtected("Token") || !e.IsProtected("Notes") || e.IsProtected(" Title ") {
		t.Fatalf("field protection mismatch: %+v", e.Fields)
	}
	if len(e.Tags) != 2 || e.Tags[0] != "Favorite" || e.Tags[1] != "work tag" || e.Created.Year() != 2024 || e.Modified.IsZero() || !e.ExpiryEnabled || e.Expires.Year() != 2024 {
		t.Fatalf("times/tags mismatch: %+v", e)
	}
	if got := parseTime(base64.StdEncoding.EncodeToString(leSeconds(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)))); !got.Equal(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("KDBX binary time decoded incorrectly: %v", got)
	}
}

func leSeconds(t time.Time) []byte {
	const unixOffset = 62135596800
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, uint64(t.Unix()+unixOffset))
	return b
}

func TestParseXMLRejectsMalformedAndMissingGroups(t *testing.T) {
	for _, tc := range []string{"<notkeepass/>", "<KeePassFile><Meta/></KeePassFile>", "<KeePassFile><Meta/><Root/></KeePassFile>", "<KeePassFile>"} {
		if _, err := parseXML([]byte(tc), ""); err == nil || !strings.Contains(err.Error(), ErrMalformed.Error()) {
			t.Fatalf("expected malformed error for %q, got %v", tc, err)
		}
	}
}

func TestParseXMLRejectsInvalidAndDuplicateIdentity(t *testing.T) {
	base := fixtureXML()
	tests := []struct {
		name string
		xml  string
	}{
		{"empty group UUID", strings.Replace(base, "<UUID>"+uuidBytes(2)+"</UUID>", "<UUID/>", 1)},
		{"zero entry UUID", strings.Replace(base, "<UUID>"+uuidBytes(6)+"</UUID>", "<UUID>AAAAAAAAAAAAAAAAAAAAAA==</UUID>", 1)},
		{"duplicate entry UUID", strings.Replace(base, "<UUID>"+uuidBytes(6)+"</UUID>", "<UUID>"+uuidBytes(5)+"</UUID>", 1)},
		{"malformed recycle UUID", strings.Replace(base, "<RecycleBinUUID>"+uuidBytes(4)+"</RecycleBinUUID>", "<RecycleBinUUID>bad</RecycleBinUUID>", 1)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := parseXML([]byte(tc.xml), ""); !errors.Is(err, ErrMalformed) {
				t.Fatalf("expected malformed identity error, got %v", err)
			}
		})
	}
}

func TestNormalizeUUIDAcceptsHexAndBase64Forms(t *testing.T) {
	want := "01010101-0101-0101-0101-010101010101"
	if got := normalizeUUID(uuidBytes(1)); got != want {
		t.Fatalf("base64 UUID = %q, want %q", got, want)
	}
	if got := normalizeUUID("01010101010101010101010101010101"); got != want {
		t.Fatalf("hex UUID = %q, want %q", got, want)
	}
}

func TestClearDropsSecretBearingSlices(t *testing.T) {
	db := &Database{Groups: []Group{{Name: "g"}}, Entries: []Entry{{Fields: []Field{{Name: "Password", Value: "secret"}}, Tags: []string{"tag"}, AttachmentNames: []string{"a"}}}}
	db.Clear()
	if len(db.Entries) != 0 || len(db.Groups) != 0 {
		t.Fatalf("Clear retained database slices: %+v", db)
	}
}
