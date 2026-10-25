package vault

import (
	"errors"
	"strings"
	"testing"
)

func TestParseHistoryAndProtectsTOTPSeeds(t *testing.T) {
	xml := strings.Replace(fixtureXML(),
		"<History><Entry/><Entry/></History>",
		"<History><Entry><UUID>"+uuidBytes(6)+"</UUID><String><Key>Title</Key><Value>old title</Value></String><String><Key>Password</Key><Value>old secret</Value></String><History><Entry><String><Key>Title</Key><Value>nested ignored</Value></String></Entry></History></Entry></History>", 1)
	xml = strings.Replace(xml,
		"<String><Key>Token</Key><Value Protected=\"True\">secret-token</Value></String>",
		"<String><Key>Token</Key><Value Protected=\"True\">secret-token</Value></String><String><Key>otp</Key><Value>otpauth://totp/demo?secret=JBSWY3DPEHPK3PXP&amp;period=45&amp;digits=6</Value></String>", 1)
	db, err := parseXML([]byte(xml), "synthetic.kdbx")
	if err != nil {
		t.Fatal(err)
	}
	e := db.Entries[0]
	if !e.HasTOTP || e.TOTPPeriodSeconds != 45 || !e.IsProtected("otp") {
		t.Fatalf("OTP metadata or protection = has=%v period=%d protected=%v", e.HasTOTP, e.TOTPPeriodSeconds, e.IsProtected("otp"))
	}
	if e.HistoryCount != 1 || len(e.History) != 1 || e.History[0].Get("Title") != "old title" || e.History[0].Get("Password") != "old secret" {
		t.Fatalf("entry history was not parsed: count=%d history=%#v", e.HistoryCount, e.History)
	}
	if len(e.History[0].History) != 0 || e.History[0].HistoryCount != 0 {
		t.Fatal("history snapshots retained nested history")
	}
	if !e.History[0].IsProtected("Password") {
		t.Fatal("history password did not inherit KeePass default protection")
	}

	keePass2 := strings.Replace(fixtureXML(),
		"<String><Key>Token</Key><Value Protected=\"True\">secret-token</Value></String>",
		"<String><Key>Token</Key><Value Protected=\"True\">secret-token</Value></String><String><Key>TimeOtp-Secret-Base32</Key><Value>JBSWY3DPEHPK3PXP</Value></String><String><Key>TimeOtp-Period</Key><Value>20</Value></String>", 1)
	legacy, err := parseXML([]byte(keePass2), "synthetic.kdbx")
	if err != nil {
		t.Fatal(err)
	}
	if !legacy.Entries[0].HasTOTP || legacy.Entries[0].TOTPPeriodSeconds != 20 || !legacy.Entries[0].IsProtected("TimeOtp-Secret-Base32") {
		t.Fatalf("KeePass2 OTP metadata/protection = %#v", legacy.Entries[0])
	}
}

func TestKeePass2TOTPSeedAliasesAreProtectedAndRecognized(t *testing.T) {
	seedNames := []string{
		"TimeOtp-Secret-Hex", "TimeOtp-Secret-Base64", "TimeOtp-Secret-Utf8",
		"KeePass2TimeOtp-Secret-Hex", "KeePass2TimeOtp-Secret-Base64", "KeePass2TimeOtp-Secret-Utf8",
	}
	for _, seedName := range seedNames {
		t.Run(seedName, func(t *testing.T) {
			fixture := strings.Replace(fixtureXML(),
				"<String><Key>Token</Key><Value Protected=\"True\">secret-token</Value></String>",
				"<String><Key>Token</Key><Value Protected=\"True\">secret-token</Value></String><String><Key>"+seedName+"</Key><Value>fixture-seed</Value></String><String><Key>KeePass2TimeOtp-Step</Key><Value>19</Value></String>", 1)
			db, err := parseXML([]byte(fixture), "synthetic.kdbx")
			if err != nil {
				t.Fatal(err)
			}
			entry := db.Entries[0]
			if !entry.HasTOTP || entry.TOTPPeriodSeconds != 19 || !entry.IsProtected(seedName) {
				t.Fatalf("seed alias metadata/protection: has=%v period=%d protected=%v", entry.HasTOTP, entry.TOTPPeriodSeconds, entry.IsProtected(seedName))
			}
		})
	}
}

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
	for _, field := range clone.Entries[0].History[0].Fields {
		if field.Name == "Password" && field.Value != "old-secret" {
			t.Fatal("clone secret unexpectedly changed when source database cleared")
		}
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

func TestSelectorsRejectAmbiguousAttachmentEntry(t *testing.T) {
	root := "01010101-0101-0101-0101-010101010101"
	group := "02020202-0202-0202-0202-020202020202"
	db := &Database{
		Groups: []Group{{UUID: root, Name: "Root"}, {UUID: group, ParentUUID: root, Name: "G"}},
		Entries: []Entry{
			{UUID: "03030303-0303-0303-0303-030303030303", GroupUUID: group, Fields: []Field{{Name: "Title", Value: "same"}}},
			{UUID: "04040404-0404-0404-0404-040404040404", GroupUUID: group, Fields: []Field{{Name: "Title", Value: "same"}}},
		},
	}
	selectors, err := selectorsFor(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = selectors.selectChange(Change{Kind: AddAttachment, EntryUUID: db.Entries[0].UUID, AttachmentName: "file.bin", AttachmentData: []byte("data")})
	if !errors.Is(err, ErrAmbiguous) {
		t.Fatalf("ambiguous attachment entry selection = %v", err)
	}
}
