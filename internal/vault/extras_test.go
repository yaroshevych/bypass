package vault

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestTotpUsesNativeShowAndReturnsBoundedMetadata(t *testing.T) {
	db, entryUUID := testMetadataDB(t, "Totp Entry")
	db.Entries[0].HasTOTP = true
	db.Entries[0].TOTPPeriodSeconds = 17
	cliPath := filepath.Join(t.TempDir(), "fake-cli")
	argsPath := filepath.Join(filepath.Dir(cliPath), "args.txt")
	// The implementation uses the source-verified standalone show TOTP output.
	script := "#!/bin/sh\nprintf '%s\\n' \"$@\" > '" + argsPath + "'\nprintf 'ABCD2\\n'\n"
	if err := os.WriteFile(cliPath, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	result, err := NewCLI(cliPath).TOTP(context.Background(), db, Credentials{NoPassword: true}, entryUUID)
	if err != nil {
		t.Fatal(err)
	}
	defer zero(result.Code)
	args, err := os.ReadFile(argsPath)
	canonical, canonicalErr := filepath.EvalSymlinks(db.Path)
	if err != nil || canonicalErr != nil || string(args) != "show\n-q\n-t\n--no-password\n"+canonical+"\n/Totp Entry\n" {
		t.Fatalf("native TOTP CLI args = %q, %v", args, err)
	}
	if string(result.Code) != "ABCD2" || result.PeriodSeconds != 17 || result.RemainingSeconds < 1 || result.RemainingSeconds > 17 {
		t.Fatalf("TOTP result = code %q period %d remaining %d", result.Code, result.PeriodSeconds, result.RemainingSeconds)
	}
}

func TestTotpRejectsTerminalControlOutput(t *testing.T) {
	for name, output := range map[string]string{
		"ansi escape":      "\\033[31m123456\\033[0m",
		"embedded newline": "123\\n456",
		"unicode":          "１２３４５６",
	} {
		t.Run(name, func(t *testing.T) {
			db, entryUUID := testMetadataDB(t, "TOTP Safety")
			db.Entries[0].HasTOTP = true
			db.Entries[0].TOTPPeriodSeconds = 30
			cliPath := filepath.Join(t.TempDir(), "fake-cli")
			script := "#!/bin/sh\nprintf '%b' '" + output + "'\n"
			if err := os.WriteFile(cliPath, []byte(script), 0700); err != nil {
				t.Fatal(err)
			}
			if result, err := NewCLI(cliPath).TOTP(context.Background(), db, Credentials{NoPassword: true}, entryUUID); !errors.Is(err, ErrWrite) || len(result.Code) != 0 {
				zero(result.Code)
				t.Fatalf("unsafe TOTP CLI output result=%q err=%v", result.Code, err)
			}
		})
	}
}

func TestTotpAndAttachmentReadsRejectStaleRevision(t *testing.T) {
	db, entryUUID := testMetadataDB(t, "Entry")
	db.Entries[0].HasTOTP = true
	db.Entries[0].TOTPPeriodSeconds = 30
	if err := os.WriteFile(db.Path, []byte("external revision"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewCLI("unused").TOTP(context.Background(), db, Credentials{NoPassword: true}, entryUUID); !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("TOTP stale revision error = %v", err)
	}
	if _, err := NewCLI("unused").ExportAttachment(context.Background(), db, Credentials{NoPassword: true}, entryUUID, "file.bin"); !errors.Is(err, ErrExternalChanged) {
		t.Fatalf("attachment stale revision error = %v", err)
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

func TestRealCLIAttachmentTransactionsAndBinaryExport(t *testing.T) {
	cli := installedExtrasCLI(t)
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "attachment fixture.kdbx")
	backupDir := filepath.Join(dir, "backups")
	creds := Credentials{Password: []byte("temporary attachment integration password")}
	db, err := cli.CreateWithOptions(ctx, dbPath, creds, CreateOptions{DecryptionTimeMS: 100})
	if err != nil {
		t.Fatalf("create synthetic vault: %v", err)
	}
	defer func() {
		if db != nil {
			db.Clear()
		}
	}()
	defer creds.Clear()
	withEntry, err := cli.Apply(ctx, db, creds, Change{Kind: AddEntry, GroupUUID: db.RootUUID, Values: map[string]string{"Title": "Attachment Fixture"}}, backupDir)
	if err != nil {
		t.Fatalf("add synthetic entry: %v", err)
	}
	db.Clear()
	db = withEntry
	entryUUID := entryIDByTitle(db, db.RootUUID, "Attachment Fixture")
	if entryUUID == "" {
		t.Fatal("synthetic entry missing after add")
	}

	first := []byte{0, 0xff, 0x41, 0x00, 0x80, 'x'}
	defer zero(first)
	updated, err := cli.Apply(ctx, db, creds, Change{Kind: AddAttachment, EntryUUID: entryUUID, AttachmentName: "binary file.bin", AttachmentData: first}, backupDir)
	if err != nil {
		t.Fatalf("add attachment: %v", err)
	}
	db.Clear()
	db = updated
	exported, err := cli.ExportAttachment(ctx, db, creds, entryUUID, "binary file.bin")
	if err != nil || !bytes.Equal(exported, first) {
		zero(exported)
		t.Fatalf("binary attachment export = %v, %v", exported, err)
	}
	zero(exported)
	before := db.Revision
	if _, err := cli.Apply(ctx, db, creds, Change{Kind: AddAttachment, EntryUUID: entryUUID, AttachmentName: "binary file.bin", AttachmentData: []byte("clobber")}, backupDir); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("duplicate add should not overwrite attachment, got %v", err)
	}
	if got, err := Revision(dbPath); err != nil || got != before {
		t.Fatal("duplicate add changed the encrypted vault")
	}

	second := []byte{0xfe, 0x00, 'r', 'e', 'p', 'l', 'a', 'c', 'e'}
	defer zero(second)
	updated, err = cli.Apply(ctx, db, creds, Change{Kind: ReplaceAttachment, EntryUUID: entryUUID, AttachmentName: "binary file.bin", AttachmentData: second}, backupDir)
	if err != nil {
		t.Fatalf("replace attachment: %v", err)
	}
	db.Clear()
	db = updated
	exported, err = cli.ExportAttachment(ctx, db, creds, entryUUID, "binary file.bin")
	if err != nil || !bytes.Equal(exported, second) {
		zero(exported)
		t.Fatalf("replaced attachment export = %v, %v", exported, err)
	}
	zero(exported)

	updated, err = cli.Apply(ctx, db, creds, Change{Kind: RemoveAttachment, EntryUUID: entryUUID, AttachmentName: "binary file.bin"}, backupDir)
	if err != nil {
		t.Fatalf("remove attachment: %v", err)
	}
	db.Clear()
	db = updated
	if containsString(entryByID(db, entryUUID).AttachmentNames, "binary file.bin") {
		t.Fatal("removed attachment remains in entry metadata")
	}
	if _, err := cli.ExportAttachment(ctx, db, creds, entryUUID, "binary file.bin"); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("export after remove = %v", err)
	}
	if backups, err := os.ReadDir(backupDir); err != nil || len(backups) < 4 {
		t.Fatalf("attachment transaction backups = %d, %v", len(backups), err)
	}
	if info, err := os.Stat(backupDir); err != nil || info.Mode().Perm()&0077 != 0 {
		t.Fatal("attachment backups are not private")
	}
}

func TestNativeImportedEntryPreservesHistoryOTPAndMetadataAcrossChanges(t *testing.T) {
	cli := installedExtrasCLI(t)
	ctx := context.Background()
	dir := t.TempDir()
	xmlPath := filepath.Join(dir, "synthetic.xml")
	dbPath := filepath.Join(dir, "synthetic.kdbx")
	sourcePath := filepath.Join(dir, "source.kdbx")
	backupDir := filepath.Join(dir, "backups")
	password := []byte("synthetic imported vault password")
	newPassword := []byte("synthetic rotated vault password")
	var entryUUID string
	attachment := []byte{0x00, 0xff, 0x41, 0x00, 0x80, 0x78}
	defer zero(password)
	defer zero(newPassword)
	seed, err := cli.Create(ctx, sourcePath, Credentials{Password: password})
	if err != nil {
		t.Fatalf("create native XML export source: %v", err)
	}
	seed, err = cli.Apply(ctx, seed, Credentials{Password: password}, Change{Kind: AddEntry, GroupUUID: seed.RootUUID, Values: map[string]string{"Title": "Compatibility Entry", "UserName": "fixture-user"}}, filepath.Join(dir, "source-backups"))
	if err != nil {
		t.Fatalf("seed native XML export source: %v", err)
	}
	entryUUID = entryIDByTitle(seed, seed.RootUUID, "Compatibility Entry")
	if entryUUID == "" {
		t.Fatal("could not resolve synthetic native entry UUID")
	}
	seed, err = cli.Apply(ctx, seed, Credentials{Password: password}, Change{Kind: EditEntry, EntryUUID: entryUUID, Values: map[string]string{"Notes": "keep these notes"}}, filepath.Join(dir, "source-backups"))
	if err != nil {
		t.Fatalf("create native history snapshot: %v", err)
	}
	seed, err = cli.Apply(ctx, seed, Credentials{Password: password}, Change{Kind: AddAttachment, EntryUUID: entryUUID, AttachmentName: "original.bin", AttachmentData: attachment}, filepath.Join(dir, "source-backups"))
	if err != nil {
		t.Fatalf("add native fixture attachment: %v", err)
	}
	seed.Clear()
	cmdExport := exec.CommandContext(ctx, cli.binary, "export", "-q", "-f", "xml", sourcePath)
	cmdExport.Stdin = bytes.NewReader(append(append([]byte(nil), password...), '\n'))
	var exportedXML bytes.Buffer
	cmdExport.Stdout, cmdExport.Stderr = &exportedXML, &bytes.Buffer{}
	if err := cmdExport.Run(); err != nil {
		t.Fatalf("export native XML baseline: %v", err)
	}
	fixture := exportedXML.String()
	titleAt := strings.Index(fixture, "<Value>Compatibility Entry</Value>")
	if titleAt < 0 {
		t.Fatal("native export missing synthetic entry title")
	}
	uuidStart := strings.LastIndex(fixture[:titleAt], "<UUID>")
	uuidEnd := strings.Index(fixture[uuidStart:], "</UUID>")
	if uuidStart < 0 || uuidEnd < 0 {
		t.Fatal("native export missing synthetic entry UUID")
	}
	entryUUIDXML := fixture[uuidStart+len("<UUID>") : uuidStart+uuidEnd]
	if !strings.Contains(fixture, "<History>") {
		t.Fatal("native export missing entry history")
	}
	fixture = strings.Replace(fixture, "<History>", `<String><Key>otp</Key><Value ProtectInMemory="True">otpauth://totp/Test?secret=JBSWY3DPEHPK3PXP&amp;period=45</Value></String><String><Key>CustomProtected</Key><Value ProtectInMemory="True">fixture-custom-secret</Value></String><History>`, 1)
	if strings.Contains(fixture, "<Tags/>") {
		fixture = strings.Replace(fixture, "<Tags/>", "<Tags>finance, synthetic</Tags>", 1)
	} else if strings.Contains(fixture, "<Tags></Tags>") {
		fixture = strings.Replace(fixture, "<Tags></Tags>", "<Tags>finance, synthetic</Tags>", 1)
	}
	entryUUID = normalizeUUID(entryUUIDXML)
	if err := os.WriteFile(xmlPath, []byte(fixture), 0600); err != nil {
		t.Fatal(err)
	}
	input := append(append([]byte(nil), password...), '\n')
	input = append(input, password...)
	input = append(input, '\n')
	defer zero(input)
	cmd := exec.CommandContext(ctx, cli.binary, "import", "-q", "--set-password", "--decryption-time", "100", xmlPath, dbPath)
	cmd.Stdin = bytes.NewReader(input)
	cmd.Stdout, cmd.Stderr = &bytes.Buffer{}, &bytes.Buffer{}
	if err := cmd.Run(); err != nil {
		t.Fatalf("import synthetic XML fixture: %v; output %q; stderr %q", err, cmd.Stdout, cmd.Stderr)
	}
	creds := Credentials{Password: password}
	db, err := cli.Open(ctx, dbPath, creds)
	if err != nil {
		t.Fatalf("open imported fixture: %v", err)
	}
	defer func() {
		if db != nil {
			db.Clear()
		}
	}()
	entry := entryByID(db, normalizeUUID(entryUUID))
	if entry == nil || !entry.HasTOTP || entry.TOTPPeriodSeconds != 45 || !entry.IsProtected("otp") || !entry.IsProtected("CustomProtected") {
		t.Fatalf("imported OTP/protection metadata lost: %#v", entry)
	}
	if entry.HistoryCount < 1 || len(entry.History) < 1 || entry.History[0].Get("Title") != "Compatibility Entry" {
		t.Fatalf("imported history/protection lost: count=%d history=%#v", entry.HistoryCount, entry.History)
	}
	if !entry.History[0].IsProtected("Password") || len(entry.Tags) != 2 || entry.ExpiryEnabled {
		t.Fatalf("imported unrelated entry data lost: %#v", entry)
	}
	original, err := cli.ExportAttachment(ctx, db, creds, entry.UUID, "original.bin")
	if err != nil || !bytes.Equal(original, attachment) {
		zero(original)
		t.Fatalf("imported attachment bytes = %v, %v", original, err)
	}
	zero(original)

	updated, err := cli.Apply(ctx, db, creds, Change{Kind: EditEntry, EntryUUID: entry.UUID, Values: map[string]string{"Title": "Edited title", "Notes": "edited notes"}}, backupDir)
	if err != nil {
		t.Fatalf("edit imported fixture entry: %v", err)
	}
	db.Clear()
	db = updated
	updated, err = cli.ChangeCredentials(ctx, db, creds, Credentials{Password: newPassword}, backupDir)
	if err != nil {
		t.Fatalf("rotate imported fixture credentials: %v", err)
	}
	db.Clear()
	db = updated
	added := []byte{0x90, 0x00, 0xfe, 0x10}
	defer zero(added)
	updated, err = cli.Apply(ctx, db, Credentials{Password: newPassword}, Change{Kind: AddAttachment, EntryUUID: entry.UUID, AttachmentName: "added.bin", AttachmentData: added}, backupDir)
	if err != nil {
		t.Fatalf("add attachment to imported fixture: %v", err)
	}
	db.Clear()
	db = updated
	db.Clear()
	db, err = cli.Open(ctx, dbPath, Credentials{Password: newPassword})
	if err != nil {
		t.Fatalf("reopen changed fixture with rotated credentials: %v", err)
	}
	entry = entryByID(db, normalizeUUID(entryUUID))
	if entry == nil || entry.Get("Title") != "Edited title" || entry.Get("Notes") != "edited notes" || entry.Get("UserName") != "fixture-user" || entry.Get("CustomProtected") != "fixture-custom-secret" || !entry.IsProtected("CustomProtected") {
		t.Fatalf("edited entry or preserved custom data lost: %#v", entry)
	}
	if !entry.HasTOTP || entry.TOTPPeriodSeconds != 45 || entry.HistoryCount < 1 || len(entry.History) < 1 || entry.History[0].Get("Title") != "Compatibility Entry" || len(entry.Tags) != 2 || entry.ExpiryEnabled {
		t.Fatalf("unrelated OTP/history/tags/expiry data changed: %#v", entry)
	}
	for name, want := range map[string][]byte{"original.bin": attachment, "added.bin": added} {
		got, err := cli.ExportAttachment(ctx, db, Credentials{Password: newPassword}, entry.UUID, name)
		if err != nil || !bytes.Equal(got, want) {
			zero(got)
			t.Fatalf("reopened attachment %q = %v, %v", name, got, err)
		}
		zero(got)
	}
}

func testMetadataDB(t *testing.T, title string) (*Database, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "metadata-fixture.kdbx")
	if err := os.WriteFile(path, []byte("synthetic encrypted placeholder"), 0600); err != nil {
		t.Fatal(err)
	}
	revision, err := Revision(path)
	if err != nil {
		t.Fatal(err)
	}
	rootUUID := "01010101-0101-0101-0101-010101010101"
	entryUUID := "02020202-0202-0202-0202-020202020202"
	db := &Database{
		Path: path, Revision: revision, RootUUID: rootUUID,
		Groups:  []Group{{UUID: rootUUID, Name: "Root"}},
		Entries: []Entry{{UUID: entryUUID, GroupUUID: rootUUID, Fields: []Field{{Name: "Title", Value: title}}, AttachmentNames: []string{"file.bin"}}},
	}
	return db, entryUUID
}

func installedExtrasCLI(t *testing.T) *CLI {
	t.Helper()
	cli := NewCLI("")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Probe(ctx); err != nil {
		t.Skip("keepassxc-cli is not installed")
	}
	return cli
}
