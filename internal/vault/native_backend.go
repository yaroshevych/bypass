package vault

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	gok "github.com/tobischo/gokeepasslib/v3"
	"github.com/tobischo/gokeepasslib/v3/wrappers"
)

const defaultArgon2Iterations = 2

// NativeBackend reads and edits KDBX files in process. It retains the decoded
// database only for the lifetime of an unlocked Database.
type NativeBackend struct{}

// NewBackend creates the in-process KeePass backend.
func NewBackend() *NativeBackend { return &NativeBackend{} }

type nativeState struct {
	mu              sync.Mutex
	db              *gok.Database
	path            string
	protectedLocked bool
	writeBlocked    bool
}

type nativeSelection struct {
	group *Group
	entry *Entry
}

var nativePathLocks sync.Map

func lockNativePath(path string) func() {
	lock, _ := nativePathLocks.LoadOrStore(path, &sync.Mutex{})
	mutex := lock.(*sync.Mutex)
	mutex.Lock()
	return mutex.Unlock
}

func modelGroup(db *Database, id string) *Group {
	for i := range db.Groups {
		if normalizeUUID(db.Groups[i].UUID) == normalizeUUID(id) {
			return &db.Groups[i]
		}
	}
	return nil
}
func modelEntry(db *Database, id string) *Entry {
	for i := range db.Entries {
		if normalizeUUID(db.Entries[i].UUID) == normalizeUUID(id) {
			return &db.Entries[i]
		}
	}
	return nil
}

func (s *nativeState) clear() {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearLocked()
}

func (s *nativeState) clearIfOwner(db *Database) {
	if s == nil || db == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if db.nativeOwner {
		s.clearLocked()
		db.nativeOwner = false
	}
	db.native = nil
}

func (s *nativeState) clearLocked() {
	if s.db != nil {
		if !s.protectedLocked {
			_ = s.db.LockProtectedEntries()
			s.protectedLocked = true
		}
		scrubNativeDatabase(s.db)
		s.db = nil
	}
}

func scrubNativeDatabase(db *gok.Database) {
	if db == nil {
		return
	}
	var scrubEntry func(*gok.Entry)
	var scrubGroup func(*gok.Group)
	scrubEntry = func(e *gok.Entry) {
		for i := range e.Values {
			e.Values[i].Value.Content = ""
		}
		for i := range e.Binaries {
			if bin := e.Binaries[i].Find(db); bin != nil {
				clear(bin.Content)
			}
		}
		for h := range e.Histories {
			for i := range e.Histories[h].Entries {
				scrubEntry(&e.Histories[h].Entries[i])
			}
		}
	}
	scrubGroup = func(g *gok.Group) {
		for i := range g.Entries {
			scrubEntry(&g.Entries[i])
		}
		for i := range g.Groups {
			scrubGroup(&g.Groups[i])
		}
	}
	if db.Content != nil {
		if db.Content.Root != nil {
			for i := range db.Content.Root.Groups {
				scrubGroup(&db.Content.Root.Groups[i])
			}
		}
		if db.Content.Meta != nil {
			for i := range db.Content.Meta.Binaries {
				clear(db.Content.Meta.Binaries[i].Content)
			}
			db.Content.RawData = nil
			if db.Content.InnerHeader != nil {
				clear(db.Content.InnerHeader.InnerRandomStreamKey)
				for i := range db.Content.InnerHeader.Binaries {
					clear(db.Content.InnerHeader.Binaries[i].Content)
				}
			}
		}
	}
	if db.Credentials != nil {
		clear(db.Credentials.Passphrase)
		clear(db.Credentials.Key)
		clear(db.Credentials.Windows)
	}
	if db.Header != nil && db.Header.FileHeaders != nil {
		clear(db.Header.FileHeaders.MasterSeed)
		clear(db.Header.FileHeaders.TransformSeed)
		clear(db.Header.FileHeaders.EncryptionIV)
		clear(db.Header.FileHeaders.ProtectedStreamKey)
		clear(db.Header.FileHeaders.StreamStartBytes)
		if db.Header.FileHeaders.KdfParameters != nil {
			clear(db.Header.FileHeaders.KdfParameters.SecretKey)
			clear(db.Header.FileHeaders.KdfParameters.AssocData)
		}
	}
}

func cloneNativeDatabase(db *gok.Database) (*gok.Database, error) {
	if db == nil || db.Credentials == nil {
		return nil, ErrUnlock
	}
	var encrypted bytes.Buffer
	if err := gok.NewEncoder(&encrypted).Encode(db); err != nil {
		return nil, ErrWrite
	}
	defer zero(encrypted.Bytes())
	copyDB := gok.NewDatabase()
	copyDB.Credentials = &gok.DBCredentials{Passphrase: append([]byte(nil), db.Credentials.Passphrase...), Key: append([]byte(nil), db.Credentials.Key...), Windows: append([]byte(nil), db.Credentials.Windows...)}
	if err := gok.NewDecoder(bytes.NewReader(encrypted.Bytes())).Decode(copyDB); err != nil {
		scrubNativeDatabase(copyDB)
		return nil, ErrWrite
	}
	return copyDB, nil
}

func (s *nativeState) lockProtected() {
	if s != nil && s.db != nil && !s.protectedLocked && s.db.LockProtectedEntries() == nil {
		s.protectedLocked = true
	}
}

func isTOTPSeedField(name string) bool {
	if name == "TOTP Seed" || name == "otp" || name == "TOTP" {
		return true
	}
	return strings.HasPrefix(name, "TimeOtp-Secret-") || strings.HasPrefix(name, "KeePass2TimeOtp-Secret-")
}
func supportedTOTPField(name string) bool {
	if isTOTPSeedField(name) {
		return true
	}
	switch name {
	case "Period", "Digits", "Algorithm", "TimeOtp-Period", "TimeOtp-Step", "KeePass2TimeOtp-Period", "KeePass2TimeOtp-Step", "TimeOtp-Algorithm", "KeePass2TimeOtp-Algorithm":
		return true
	}
	return false
}
func protectTOTPFields(db *gok.Database) {
	var walk func(*gok.Group)
	var protect func(*gok.Entry)
	protect = func(e *gok.Entry) {
		for i := range e.Values {
			if isTOTPSeedField(e.Values[i].Key) {
				e.Values[i].Value.Protected.Bool = true
			}
		}
		for i := range e.Histories {
			for j := range e.Histories[i].Entries {
				protect(&e.Histories[i].Entries[j])
			}
		}
	}
	walk = func(g *gok.Group) {
		for i := range g.Entries {
			protect(&g.Entries[i])
		}
		for i := range g.Groups {
			walk(&g.Groups[i])
		}
	}
	for i := range db.Content.Root.Groups {
		walk(&db.Content.Root.Groups[i])
	}
}

func hasUnsupportedXML(raw []byte, kdbx4 bool) bool {
	if len(raw) == 0 {
		return false
	}
	if kdbx4 {
		offset := 0
		for {
			if len(raw)-offset < 5 {
				return true
			}
			kind := raw[offset]
			length := uint64(binary.LittleEndian.Uint32(raw[offset+1 : offset+5]))
			offset += 5
			if length > uint64(len(raw)-offset) {
				return true
			}
			offset += int(length)
			if kind == 0 {
				raw = raw[offset:]
				break
			}
		}
	}
	if !bytes.Contains(raw, []byte("<KeePassFile")) {
		return true
	}
	known := map[string]bool{}
	for _, name := range strings.Fields(`KeePassFile Meta Generator SettingsChanged HeaderHash DatabaseName DatabaseNameChanged DatabaseDescription DatabaseDescriptionChanged DefaultUserName DefaultUserNameChanged MaintenanceHistoryDays Color MasterKeyChanged MasterKeyChangeRec MasterKeyChangeForce MemoryProtection ProtectTitle ProtectUserName ProtectPassword ProtectURL ProtectNotes ProtectAutoType CustomIcons Icon Data RecycleBinEnabled RecycleBinUUID RecycleBinChanged EntryTemplatesGroup EntryTemplatesGroupChanged HistoryMaxItems HistoryMaxSize LastSelectedGroup LastTopVisibleGroup Binaries Binary ID Compressed Root Group UUID Name Notes IconID CustomIconUUID Times CreationTime LastModificationTime LastAccessTime ExpiryTime Expires UsageCount LocationChanged IsExpanded DefaultAutoTypeSequence EnableAutoType EnableSearching LastTopVisibleEntry Tags CustomData Item Key Value String AutoType Enabled DataTransferObfuscation DefaultSequence Association Window KeystrokeSequence History Entry OverrideURL ForegroundColor BackgroundColor DeletedObjects DeletedObject DeletionTime IconID Ref`) {
		known[name] = true
	}
	knownAttrs := map[string]bool{"Protected": true, "Compressed": true, "ID": true, "Ref": true}
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	for {
		tok, err := decoder.Token()
		if err == io.EOF {
			return false
		}
		if err != nil {
			return true
		}
		switch item := tok.(type) {
		case xml.StartElement:
			if !known[item.Name.Local] || item.Name.Space != "" {
				return true
			}
			for _, attr := range item.Attr {
				if !knownAttrs[attr.Name.Local] || attr.Name.Space != "" {
					return true
				}
			}
		}
	}
}

func (b *NativeBackend) Open(ctx context.Context, path string, creds Credentials) (*Database, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if err := validateCredentials(creds); err != nil {
		return nil, err
	}
	if creds.NoPassword && creds.KeyFile == "" {
		return nil, ErrUnlock
	}
	canonical, err := canonicalPath(path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	info, err := os.Stat(canonical)
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrInvalidPath
	}
	file, err := os.Open(canonical)
	if err != nil {
		return nil, ErrInvalidPath
	}
	defer file.Close()
	native := gok.NewDatabase()
	native.Credentials, err = nativeCredentials(creds)
	if err != nil {
		return nil, ErrUnlock
	}
	if err := gok.NewDecoder(file).Decode(native); err != nil {
		scrubNativeDatabase(native)
		return nil, ErrUnlock
	}
	if err := native.UnlockProtectedEntries(); err != nil {
		scrubNativeDatabase(native)
		return nil, ErrUnlock
	}
	if err := contextError(ctx); err != nil {
		_ = native.LockProtectedEntries()
		scrubNativeDatabase(native)
		return nil, err
	}
	if native.Content == nil || native.Content.Root == nil || native.Content.Meta == nil {
		scrubNativeDatabase(native)
		return nil, ErrMalformed
	}
	protectTOTPFields(native)
	state := &nativeState{db: native, path: canonical, writeBlocked: hasUnsupportedXML(native.Content.RawData, native.Header.IsKdbx4())}
	db, err := nativeModel(state)
	if err != nil {
		state.clear()
		return nil, err
	}
	if err := native.LockProtectedEntries(); err != nil {
		state.clear()
		return nil, ErrMalformed
	}
	state.protectedLocked = true
	if err := contextError(ctx); err != nil {
		db.Clear()
		return nil, err
	}
	db.Path, db.nativeOwner = canonical, true
	db.Revision, err = Revision(canonical)
	if err != nil {
		db.Clear()
		return nil, err
	}
	return db, nil
}

func nativeCredentials(creds Credentials) (*gok.DBCredentials, error) {
	if creds.NoPassword && creds.KeyFile == "" {
		return nil, ErrUnlock
	}
	result := &gok.DBCredentials{}
	if !creds.NoPassword {
		passwordHash := sha256.Sum256(creds.Password)
		result.Passphrase = append([]byte(nil), passwordHash[:]...)
	}
	if creds.KeyFile != "" {
		key, err := gok.ParseKeyFile(creds.KeyFile)
		if err != nil {
			clear(result.Passphrase)
			return nil, err
		}
		result.Key = key
	}
	return result, nil
}

func nativeModel(state *nativeState) (*Database, error) {
	if state == nil || state.db == nil || state.db.Content == nil || state.db.Content.Root == nil {
		return nil, ErrUnlock
	}
	d := &Database{native: state, Name: filepath.Base(state.path), RecycleBinEnabled: state.db.Content.Meta.RecycleBinEnabled.Bool}
	meta := state.db.Content.Meta
	d.Name = meta.DatabaseName
	d.RecycleBinUUID = uuidString(meta.RecycleBinUUID)
	rootGroups := state.db.Content.Root.Groups
	var visit func(g *gok.Group, parent, path string, recycled bool)
	visit = func(g *gok.Group, parent, path string, recycled bool) {
		id := uuidString(g.UUID)
		if d.RootUUID == "" && parent == "" {
			d.RootUUID = id
		}
		gpath := path
		if parent != "" {
			gpath = strings.TrimSuffix(path, "/") + "/" + g.Name + "/"
		}
		isRecycled := recycled || id == d.RecycleBinUUID
		if g.Name == "" {
			d.Groups = append(d.Groups, Group{UUID: id, ParentUUID: parent, Path: gpath, Recycled: isRecycled, SearchEnabled: true, Name: ""})
		} else {
			d.Groups = append(d.Groups, Group{UUID: id, ParentUUID: parent, Path: gpath, Name: g.Name, Recycled: isRecycled, SearchEnabled: g.EnableSearching.Bool})
		}
		for i := range g.Entries {
			e := &g.Entries[i]
			d.Entries = append(d.Entries, nativeEntry(e, id, gpath, isRecycled, false))
		}
		for i := range g.Groups {
			visit(&g.Groups[i], id, gpath, isRecycled)
		}
	}
	for i := range rootGroups {
		visit(&rootGroups[i], "", "/", false)
	}
	if d.RootUUID == "" {
		return nil, ErrMalformed
	}
	return d, nil
}

func uuidString(id gok.UUID) string { raw, _ := id.MarshalText(); return normalizeUUID(string(raw)) }

func nativeEntry(e *gok.Entry, gid, path string, recycled, details bool) Entry {
	created, modified, expires := time.Time{}, time.Time{}, time.Time{}
	if e.Times.CreationTime != nil {
		created = e.Times.CreationTime.Time
	}
	if e.Times.LastModificationTime != nil {
		modified = e.Times.LastModificationTime.Time
	}
	if e.Times.ExpiryTime != nil {
		expires = e.Times.ExpiryTime.Time
	}
	out := Entry{UUID: uuidString(e.UUID), GroupUUID: gid, GroupPath: path, Recycled: recycled, Created: created, Modified: modified, Expires: expires, ExpiryEnabled: e.Times.Expires.Bool, DetailsLoaded: details, Tags: splitTags(e.Tags), HistoryCount: len(e.Histories)}
	for _, bin := range e.Binaries {
		out.AttachmentNames = append(out.AttachmentNames, bin.Name)
	}
	for _, v := range e.Values {
		if isTOTPSeedField(v.Key) {
			out.HasTOTP = v.Value.Content != ""
		}
		if v.Key == "Period" || v.Key == "TimeOtp-Period" || v.Key == "TimeOtp-Step" || v.Key == "KeePass2TimeOtp-Period" || v.Key == "KeePass2TimeOtp-Step" {
			var p int
			_, _ = fmt.Sscan(v.Value.Content, &p)
			out.TOTPPeriodSeconds = p
		}
		if details || (!v.Value.Protected.Bool && standardField(v.Key)) {
			out.Fields = append(out.Fields, Field{Name: v.Key, Value: v.Value.Content, Protected: v.Value.Protected.Bool})
		}
	}
	if out.TOTPPeriodSeconds == 0 {
		for _, v := range e.Values {
			if v.Key == "otp" || v.Key == "TOTP" {
				if parsed, err := url.Parse(v.Value.Content); err == nil {
					_, _ = fmt.Sscan(parsed.Query().Get("period"), &out.TOTPPeriodSeconds)
				}
			}
		}
	}
	for _, history := range e.Histories {
		for i := range history.Entries {
			snapshot := nativeEntry(&history.Entries[i], gid, path, recycled, true)
			snapshot.HistoryCount = 0
			snapshot.History = nil
			out.History = append(out.History, snapshot)
		}
	}
	if !details {
		out.DetailsLoaded = false
	}
	return out
}

func standardField(key string) bool {
	switch key {
	case "Title", "UserName", "URL", "Notes":
		return true
	}
	return false
}

func findNativeEntry(db *gok.Database, id string) (*gok.Group, *gok.Entry) {
	var walk func(*gok.Group) (*gok.Group, *gok.Entry)
	walk = func(g *gok.Group) (*gok.Group, *gok.Entry) {
		for i := range g.Entries {
			if uuidString(g.Entries[i].UUID) == normalizeUUID(id) {
				return g, &g.Entries[i]
			}
		}
		for i := range g.Groups {
			if pg, e := walk(&g.Groups[i]); e != nil {
				return pg, e
			}
		}
		return nil, nil
	}
	for i := range db.Content.Root.Groups {
		if g, e := walk(&db.Content.Root.Groups[i]); e != nil {
			return g, e
		}
	}
	return nil, nil
}

func findNativeGroup(db *gok.Database, id string) *gok.Group {
	var walk func(*gok.Group) *gok.Group
	walk = func(g *gok.Group) *gok.Group {
		if uuidString(g.UUID) == normalizeUUID(id) {
			return g
		}
		for i := range g.Groups {
			if got := walk(&g.Groups[i]); got != nil {
				return got
			}
		}
		return nil
	}
	for i := range db.Content.Root.Groups {
		if got := walk(&db.Content.Root.Groups[i]); got != nil {
			return got
		}
	}
	return nil
}

func (b *NativeBackend) ResolveField(ctx context.Context, db *Database, _ Credentials, entryID, field string) ([]byte, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if field == "TOTP" {
		result, err := b.TOTP(ctx, db, Credentials{}, entryID)
		return result.Code, err
	}
	if db == nil || db.native == nil {
		return nil, ErrUnlock
	}
	s := db.native
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnlock
	}
	_, e := findNativeEntry(s.db, entryID)
	if e == nil {
		return nil, ErrUnsupported
	}
	if s.protectedLocked {
		if err := s.db.UnlockProtectedEntries(); err != nil {
			return nil, ErrMalformed
		}
		s.protectedLocked = false
	}
	defer s.lockProtected()
	_, e = findNativeEntry(s.db, entryID)
	for _, v := range e.Values {
		if v.Key == field {
			return []byte(v.Value.Content), nil
		}
	}
	return nil, ErrUnsupported
}

func (b *NativeBackend) EntryDetails(ctx context.Context, db *Database, entryID string) ([]Field, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if db == nil || db.native == nil {
		return nil, ErrUnlock
	}
	s := db.native
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnlock
	}
	_, e := findNativeEntry(s.db, entryID)
	if e == nil {
		return nil, ErrUnsupported
	}
	if s.protectedLocked {
		if err := s.db.UnlockProtectedEntries(); err != nil {
			return nil, ErrMalformed
		}
		s.protectedLocked = false
	}
	defer s.lockProtected()
	_, e = findNativeEntry(s.db, entryID)
	out := make([]Field, 0, len(e.Values))
	for _, v := range e.Values {
		out = append(out, Field{Name: v.Key, Value: v.Value.Content, Protected: v.Value.Protected.Bool})
	}
	return out, nil
}

func (b *NativeBackend) Generate(ctx context.Context, length int) ([]byte, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if length < 1 || length > maxGeneratedPasswordLength {
		return nil, ErrUnsupported
	}
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789!@#$%^&*()-_=+"
	buf := make([]byte, length)
	if _, err := io.ReadFull(randReader{}, buf); err != nil {
		return nil, ErrWrite
	}
	for i := range buf {
		buf[i] = alphabet[int(buf[i])%len(alphabet)]
	}
	return buf, nil
}

type randReader struct{}

func (randReader) Read(p []byte) (int, error) { return rand.Read(p) }

func (b *NativeBackend) CreateWithOptions(ctx context.Context, path string, creds Credentials, options CreateOptions) (*Database, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if creds.YubiKey != "" {
		return nil, ErrUnsupported
	}
	if err := validateNewCredentials(creds); err != nil {
		return nil, err
	}
	if options.DecryptionTimeMS != 0 && (options.DecryptionTimeMS < minKDFMillis || options.DecryptionTimeMS > maxKDFMillis) {
		return nil, ErrUnsupported
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	if _, err = os.Lstat(abs); err == nil {
		return nil, ErrAlreadyExists
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, ErrInvalidPath
	}
	db := gok.NewDatabase(gok.WithDatabaseKDBXVersion4())
	if options.DecryptionTimeMS > 0 && db.Header.FileHeaders.KdfParameters != nil {
		iterations := uint64(options.DecryptionTimeMS) * defaultArgon2Iterations / uint64(minKDFMillis)
		if iterations == 0 {
			iterations = 1
		}
		db.Header.FileHeaders.KdfParameters.Iterations = iterations
	}
	db.Credentials, err = nativeCredentials(creds)
	if err != nil {
		return nil, ErrUnlock
	}
	defer scrubNativeDatabase(db)
	db.Content.Root.Groups = nil
	root := gok.NewGroup()
	root.Name = filepath.Base(abs)
	bin := gok.NewGroup()
	bin.Name = "Recycle Bin"
	root.Groups = append(root.Groups, bin)
	db.Content.Root.Groups = append(db.Content.Root.Groups, root)
	db.Content.Meta.RecycleBinEnabled.Bool = true
	db.Content.Meta.RecycleBinUUID = bin.UUID
	db.Content.Meta.DatabaseName = root.Name
	if err := writeNativeFileExclusive(abs, db); err != nil {
		return nil, err
	}
	if err := os.Chmod(abs, 0600); err != nil {
		return nil, ErrWrite
	}
	return b.Open(ctx, abs, creds)
}

func writeNativeFile(path string, db *gok.Database) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".bypass-vault-*.kdbx")
	if err != nil {
		return ErrWrite
	}
	name := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = tmp.Close()
			_ = os.Remove(name)
		}
	}()
	if err := tmp.Chmod(0600); err != nil {
		return ErrWrite
	}
	if err := gok.NewEncoder(tmp).Encode(db); err != nil {
		return ErrWrite
	}
	if err := tmp.Sync(); err != nil {
		return ErrWrite
	}
	if err := tmp.Close(); err != nil {
		return ErrWrite
	}
	if err := os.Rename(name, path); err != nil {
		return ErrWrite
	}
	ok = true
	if d, err := os.Open(dir); err == nil {
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

func writeNativeFileExclusive(path string, db *gok.Database) error {
	dir := filepath.Dir(path)
	file, err := os.CreateTemp(dir, ".bypass-vault-create-*.kdbx")
	if err != nil {
		return ErrWrite
	}
	defer func() {
		_ = file.Close()
		_ = os.Remove(file.Name())
	}()
	if err := file.Chmod(0600); err != nil {
		return ErrWrite
	}
	if err := gok.NewEncoder(file).Encode(db); err != nil {
		return ErrWrite
	}
	if err := file.Sync(); err != nil {
		return ErrWrite
	}
	if err := file.Close(); err != nil {
		return ErrWrite
	}
	if err := os.Link(file.Name(), path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return ErrAlreadyExists
		}
		return ErrWrite
	}
	if directory, err := os.Open(dir); err == nil {
		_ = directory.Sync()
		_ = directory.Close()
	}
	return nil
}

func (b *NativeBackend) Apply(ctx context.Context, db *Database, _ Credentials, change Change, backupDir string) (*Database, error) {
	if db == nil || db.native == nil {
		return nil, ErrWrite
	}
	if err := validateChange(change); err != nil {
		return nil, err
	}
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	s := db.native
	s.mu.Lock()
	defer s.mu.Unlock()
	releasePath := lockNativePath(s.path)
	defer releasePath()
	if s.db == nil {
		return nil, ErrUnlock
	}
	if s.writeBlocked {
		return nil, ErrUnsupportedData
	}
	current, err := Revision(s.path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	if subtle.ConstantTimeCompare(current[:], db.Revision[:]) != 1 {
		return nil, ErrExternalChanged
	}
	selection := nativeSelection{}
	if change.GroupUUID != "" {
		selection.group = modelGroup(db, change.GroupUUID)
	}
	if change.EntryUUID != "" {
		selection.entry = modelEntry(db, change.EntryUUID)
	}
	switch change.Kind {
	case AddEntry, CreateGroup, MoveEntry:
		if selection.group == nil || selection.group.Recycled {
			return nil, ErrUnsupported
		}
	case EditEntry, DeleteEntry, AddAttachment, ReplaceAttachment, RemoveAttachment:
		if selection.entry == nil {
			return nil, ErrUnsupported
		}
	case DeleteGroup, PrepareDeleteGroup:
		if selection.group == nil {
			return nil, ErrUnsupported
		}
	case PrepareRecycleBin:
		if change.EntryUUID == "" && change.GroupUUID == "" {
			return nil, ErrUnsupported
		}
	}
	if change.Kind == PrepareRecycleBin {
		if !db.RecycleBinEnabled || findNativeGroup(s.db, db.RecycleBinUUID) == nil {
			return nil, ErrUnsupported
		}
		db.RecycleBinVerified = true
		return db.Clone(), nil
	}
	if change.Kind == PrepareDeleteGroup {
		if !db.RecycleBinEnabled || selection.group.Recycled {
			return nil, ErrUnsupported
		}
		plan := &GroupDeletePlan{GroupUUID: selection.group.UUID, GroupName: selection.group.Name, Revision: db.Revision}
		countNativeGroup(selection.group.UUID, db, &plan.TargetEntries, &plan.TargetGroups)
		plan.TargetGroups++ // Include the selected group itself in the confirmation count.
		db.RecycleBinVerified = true
		db.GroupDeletePlan = plan
		next := db.Clone()
		next.GroupDeletePlan = plan
		return next, nil
	}
	if db.RecycleBinEnabled && !db.RecycleBinVerified && (change.Kind == DeleteEntry && !selection.entry.Recycled || change.Kind == DeleteGroup && !selection.group.Recycled) {
		return nil, ErrUnsupported
	}
	if err := backupVault(s.path, backupDir); err != nil {
		return nil, ErrBackup
	}
	originalDB := s.db
	candidate, err := cloneNativeDatabase(originalDB)
	if err != nil {
		return nil, err
	}
	s.db = candidate
	s.protectedLocked = true
	committed := false
	defer func() {
		if !committed {
			s.db = originalDB
			s.protectedLocked = true
			scrubNativeDatabase(candidate)
		}
	}()
	if s.protectedLocked {
		if err := s.db.UnlockProtectedEntries(); err != nil {
			return nil, ErrWrite
		}
		s.protectedLocked = false
	}
	valuesLocked := false
	defer func() {
		if !valuesLocked && s.db != nil {
			s.lockProtected()
		}
	}()
	switch change.Kind {
	case AddEntry:
		if change.Values["Title"] == "" {
			return nil, ErrUnsupported
		}
		g := findNativeGroup(s.db, change.GroupUUID)
		if g == nil {
			return nil, ErrUnsupported
		}
		e := gok.NewEntry()
		fillNativeValues(&e, change.Values)
		if change.PasswordChanged || len(change.Password) > 0 {
			setNativeValue(&e, "Password", string(change.Password), true)
		}
		g.Entries = append(g.Entries, e)
	case EditEntry:
		_, e := findNativeEntry(s.db, change.EntryUUID)
		if e == nil {
			return nil, ErrUnsupported
		}
		snapshot := e.Clone()
		snapshot.Histories = nil
		e.Histories = append(e.Histories, gok.History{Entries: []gok.Entry{snapshot}})
		fillNativeValues(e, change.Values)
		if change.PasswordChanged {
			setNativeValue(e, "Password", string(change.Password), true)
		}
	case MoveEntry:
		sg, e := findNativeEntry(s.db, change.EntryUUID)
		dg := findNativeGroup(s.db, change.GroupUUID)
		if sg == nil || e == nil || dg == nil {
			return nil, ErrUnsupported
		}
		moved := *e
		removeNativeEntry(sg, e)
		dg.Entries = append(dg.Entries, moved)
	case DeleteEntry:
		g, e := findNativeEntry(s.db, change.EntryUUID)
		if g == nil || e == nil {
			return nil, ErrUnsupported
		}
		if db.RecycleBinEnabled && !selection.entry.Recycled {
			bin := findNativeGroup(s.db, db.RecycleBinUUID)
			if bin == nil {
				return nil, ErrUnsupported
			}
			moved := *e
			removeNativeEntry(g, e)
			bin.Entries = append(bin.Entries, moved)
		} else {
			removeNativeEntry(g, e)
		}
	case CreateGroup:
		if strings.TrimSpace(change.NewName) == "" || strings.ContainsAny(change.NewName, "\r\n\x00") {
			return nil, ErrUnsupported
		}
		g := findNativeGroup(s.db, change.GroupUUID)
		if g == nil {
			return nil, ErrUnsupported
		}
		child := gok.NewGroup()
		child.Name = change.NewName
		g.Groups = append(g.Groups, child)
	case DeleteGroup:
		g := findNativeGroup(s.db, change.GroupUUID)
		parent := findNativeGroup(s.db, selection.group.ParentUUID)
		if g == nil || parent == nil {
			return nil, ErrUnsupported
		}
		if db.RecycleBinEnabled && !selection.group.Recycled {
			if db.GroupDeletePlan == nil || db.GroupDeletePlan.GroupUUID != change.GroupUUID || db.GroupDeletePlan.Revision != db.Revision {
				return nil, ErrUnsupported
			}
			bin := findNativeGroup(s.db, db.RecycleBinUUID)
			if bin == nil {
				return nil, ErrUnsupported
			}
			removeNativeGroup(parent, g)
			bin.Groups = append(bin.Groups, *g)
		} else {
			removeNativeGroup(parent, g)
		}
	case AddAttachment, ReplaceAttachment, RemoveAttachment:
		_, e := findNativeEntry(s.db, change.EntryUUID)
		if e == nil {
			return nil, ErrUnsupported
		}
		if change.Kind == AddAttachment || change.Kind == ReplaceAttachment {
			for i := len(e.Binaries) - 1; i >= 0; i-- {
				if e.Binaries[i].Name == change.AttachmentName {
					e.Binaries = append(e.Binaries[:i], e.Binaries[i+1:]...)
				}
			}
			bin := s.db.AddBinary(change.AttachmentData)
			e.Binaries = append(e.Binaries, bin.CreateReference(change.AttachmentName))
		} else {
			found := false
			for i := len(e.Binaries) - 1; i >= 0; i-- {
				if e.Binaries[i].Name == change.AttachmentName {
					e.Binaries = append(e.Binaries[:i], e.Binaries[i+1:]...)
					found = true
				}
			}
			if !found {
				return nil, ErrUnsupported
			}
		}
	}
	if err := s.db.LockProtectedEntries(); err != nil {
		return nil, ErrWrite
	}
	s.protectedLocked = true
	valuesLocked = true
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	current, err = Revision(s.path)
	if err != nil || subtle.ConstantTimeCompare(current[:], db.Revision[:]) != 1 {
		return nil, ErrExternalChanged
	}
	if err := writeNativeFile(s.path, s.db); err != nil {
		return nil, err
	}
	committed = true
	scrubNativeDatabase(originalDB)
	fresh, err := b.modelUnderLock(db)
	if err != nil {
		return nil, err
	}
	fresh.Revision, err = Revision(s.path)
	if err != nil {
		return nil, ErrWrite
	}
	fresh.Path = s.path
	fresh.nativeOwner = true
	db.nativeOwner = false
	return fresh, nil
}

func (b *NativeBackend) modelUnderLock(old *Database) (*Database, error) {
	return nativeModel(old.native)
}
func setNativeValue(e *gok.Entry, key, value string, protected bool) {
	if key == "Tags" {
		e.Tags = value
		return
	}
	if key == "Password" || isTOTPSeedField(key) {
		protected = true
	}
	for i := range e.Values {
		if e.Values[i].Key == key {
			e.Values[i].Value.Content = value
			e.Values[i].Value.Protected.Bool = protected
			return
		}
	}
	e.Values = append(e.Values, gok.ValueData{Key: key, Value: gok.V{Content: value, Protected: wrapperBool(protected)}})
}
func wrapperBool(v bool) (out wrappers.BoolWrapper) { out.Bool = v; return }
func fillNativeValues(e *gok.Entry, values map[string]string) {
	for key, val := range values {
		setNativeValue(e, key, val, false)
	}
}
func removeNativeEntry(g *gok.Group, e *gok.Entry) {
	for i := range g.Entries {
		if &g.Entries[i] == e {
			g.Entries = append(g.Entries[:i], g.Entries[i+1:]...)
			return
		}
	}
}
func removeNativeGroup(p, g *gok.Group) {
	for i := range p.Groups {
		if &p.Groups[i] == g {
			p.Groups = append(p.Groups[:i], p.Groups[i+1:]...)
			return
		}
	}
}
func countNativeGroup(id string, db *Database, entries, groups *int) {
	for _, g := range db.Groups {
		if g.ParentUUID == id {
			*groups++
			countNativeGroup(g.UUID, db, entries, groups)
		}
	}
	for _, e := range db.Entries {
		if e.GroupUUID == id {
			*entries++
		}
	}
}
func (b *NativeBackend) ExportAttachment(ctx context.Context, db *Database, _ Credentials, entryID, name string) ([]byte, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if db == nil || db.native == nil {
		return nil, ErrUnlock
	}
	s := db.native
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return nil, ErrUnlock
	}
	_, e := findNativeEntry(s.db, entryID)
	if e == nil {
		return nil, ErrUnsupported
	}
	for _, ref := range e.Binaries {
		if ref.Name == name {
			bin := ref.Find(s.db)
			if bin == nil {
				return nil, ErrMalformed
			}
			return bin.GetContentBytes()
		}
	}
	return nil, ErrUnsupported
}

func (b *NativeBackend) InfoDatabase(ctx context.Context, db *Database) (DatabaseInfo, error) {
	if err := contextError(ctx); err != nil {
		return DatabaseInfo{}, err
	}
	if db == nil || db.native == nil {
		return DatabaseInfo{}, ErrUnlock
	}
	info := DatabaseInfo{Name: db.Name, UUID: db.RootUUID, RecycleBinEnabled: db.RecycleBinEnabled, GroupCount: len(db.Groups), EntryCount: len(db.Entries)}
	state := db.native
	state.mu.Lock()
	defer state.mu.Unlock()
	if state.db != nil && state.db.Header != nil && state.db.Header.FileHeaders != nil {
		header := state.db.Header.FileHeaders
		info.Cipher = hex.EncodeToString(header.CipherID)
		if state.db.Header.IsKdbx4() && header.KdfParameters != nil {
			info.KDF = hex.EncodeToString(header.KdfParameters.UUID)
		} else {
			info.KDF = "AES-KDF"
		}
	}
	now := time.Now()
	for _, e := range db.Entries {
		if e.ExpiryEnabled && !e.Expires.IsZero() && e.Expires.Before(now) {
			info.ExpiredEntryCount++
		}
	}
	return info, nil
}

func (b *NativeBackend) ChangeCredentials(ctx context.Context, db *Database, _ Credentials, newCreds Credentials, backupDir string) (*Database, error) {
	if err := contextError(ctx); err != nil {
		return nil, err
	}
	if db == nil || db.native == nil {
		return nil, ErrUnlock
	}
	if newCreds.YubiKey != "" {
		return nil, ErrUnsupported
	}
	if err := validateNewCredentials(newCreds); err != nil {
		return nil, err
	}
	s := db.native
	s.mu.Lock()
	defer s.mu.Unlock()
	releasePath := lockNativePath(s.path)
	defer releasePath()
	if s.writeBlocked {
		return nil, ErrUnsupportedData
	}
	revision, err := Revision(s.path)
	if err != nil {
		return nil, ErrInvalidPath
	}
	if revision != db.Revision {
		return nil, ErrExternalChanged
	}
	if err := backupVault(s.path, backupDir); err != nil {
		return nil, ErrBackup
	}
	originalDB := s.db
	candidate, err := cloneNativeDatabase(originalDB)
	if err != nil {
		return nil, err
	}
	next, err := nativeCredentials(newCreds)
	if err != nil {
		scrubNativeDatabase(candidate)
		return nil, ErrUnlock
	}
	candidate.Credentials = next
	revision, err = Revision(s.path)
	if err != nil || revision != db.Revision {
		scrubNativeDatabase(candidate)
		return nil, ErrExternalChanged
	}
	if err := contextError(ctx); err != nil {
		scrubNativeDatabase(candidate)
		return nil, err
	}
	if err := writeNativeFile(s.path, candidate); err != nil {
		scrubNativeDatabase(candidate)
		return nil, ErrWrite
	}
	s.db = candidate
	s.protectedLocked = true
	scrubNativeDatabase(originalDB)
	fresh, err := nativeModel(s)
	if err != nil {
		return nil, err
	}
	fresh.Path = s.path
	fresh.Revision, err = Revision(s.path)
	if err != nil {
		return nil, ErrWrite
	}
	fresh.nativeOwner = true
	db.nativeOwner = false
	return fresh, nil
}

func (b *NativeBackend) TOTP(ctx context.Context, db *Database, _ Credentials, entryID string) (TOTPResult, error) {
	if err := contextError(ctx); err != nil {
		return TOTPResult{}, err
	}
	fields, err := b.EntryDetails(ctx, db, entryID)
	if err != nil {
		return TOTPResult{}, err
	}
	fieldValue, seedEncoding := "", "BASE32"
	period, digits, algorithm := 30, 6, "SHA1"
	for _, field := range fields {
		switch field.Name {
		case "Period", "TimeOtp-Period", "TimeOtp-Step", "KeePass2TimeOtp-Period", "KeePass2TimeOtp-Step":
			_, _ = fmt.Sscan(field.Value, &period)
		case "Digits", "TimeOtp-Digits", "KeePass2TimeOtp-Digits":
			_, _ = fmt.Sscan(field.Value, &digits)
		case "Algorithm", "TimeOtp-Algorithm", "KeePass2TimeOtp-Algorithm":
			algorithm = strings.ToUpper(field.Value)
		default:
			if isTOTPSeedField(field.Name) {
				if fieldValue == "" {
					fieldValue = field.Value
					if strings.HasSuffix(field.Name, "-Hex") {
						seedEncoding = "HEX"
					} else if strings.HasSuffix(field.Name, "-Base64") {
						seedEncoding = "BASE64"
					} else if strings.HasSuffix(field.Name, "-Utf8") {
						seedEncoding = "UTF8"
					}
				}
			}
		}
	}
	if strings.HasPrefix(strings.ToLower(fieldValue), "otpauth://") {
		parsed, parseErr := url.Parse(fieldValue)
		if parseErr != nil || parsed.Host != "totp" {
			return TOTPResult{}, ErrUnsupported
		}
		query := parsed.Query()
		fieldValue = query.Get("secret")
		if query.Get("period") != "" {
			_, _ = fmt.Sscan(query.Get("period"), &period)
		}
		if query.Get("digits") != "" {
			_, _ = fmt.Sscan(query.Get("digits"), &digits)
		}
		if query.Get("algorithm") != "" {
			algorithm = strings.ToUpper(query.Get("algorithm"))
		}
	}
	if fieldValue == "" {
		return TOTPResult{}, ErrUnsupported
	}
	if period < 1 || period > 86400 || (digits != 6 && digits != 8) {
		return TOTPResult{}, ErrUnsupported
	}
	var seed []byte
	switch seedEncoding {
	case "HEX":
		seed, err = hex.DecodeString(strings.TrimSpace(fieldValue))
	case "BASE64":
		seed, err = base64.StdEncoding.DecodeString(strings.TrimSpace(fieldValue))
	case "UTF8":
		seed = []byte(fieldValue)
	default:
		seedText := strings.TrimRight(strings.ToUpper(strings.TrimSpace(fieldValue)), "=")
		seed, err = base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(seedText)
	}
	if err != nil {
		return TOTPResult{}, ErrUnsupported
	}
	now := time.Now()
	counter := uint64(now.Unix() / int64(period))
	var msg [8]byte
	binary.BigEndian.PutUint64(msg[:], counter)
	hash := sha1.New
	switch algorithm {
	case "SHA1":
	case "SHA256":
		hash = sha256.New
	case "SHA512":
		hash = sha512.New
	default:
		return TOTPResult{}, ErrUnsupported
	}
	mac := hmac.New(hash, seed)
	_, _ = mac.Write(msg[:])
	sum := mac.Sum(nil)
	offset := sum[len(sum)-1] & 0x0f
	divisor := uint32(1)
	for i := 0; i < digits; i++ {
		divisor *= 10
	}
	code := (binary.BigEndian.Uint32(sum[offset:offset+4]) & 0x7fffffff) % divisor
	return TOTPResult{Code: []byte(fmt.Sprintf("%0*d", digits, code)), PeriodSeconds: period, RemainingSeconds: period - int(now.Unix()%int64(period))}, nil
}

var _ Backend = (*NativeBackend)(nil)
var _ Writer = (*NativeBackend)(nil)
