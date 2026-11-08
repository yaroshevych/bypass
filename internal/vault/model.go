package vault

import (
	"bypass/internal/vaultmodel"
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
)

type Credentials struct {
	Password   []byte
	KeyFile    string
	NoPassword bool
	YubiKey    string
}

func (c Credentials) Clone() Credentials {
	c.Password = append([]byte(nil), c.Password...)
	return c
}

func (c *Credentials) Clear() {
	if c == nil {
		return
	}
	for i := range c.Password {
		c.Password[i] = 0
	}
	c.Password = nil
	c.KeyFile = ""
	c.NoPassword = false
	c.YubiKey = ""
}

type Backend interface {
	Open(context.Context, string, Credentials) (*Database, error)
}

type ChangeKind string

const (
	AddEntry           ChangeKind = "add_entry"
	EditEntry          ChangeKind = "edit_entry"
	MoveEntry          ChangeKind = "move_entry"
	DeleteEntry        ChangeKind = "delete_entry"
	CreateGroup        ChangeKind = "create_group"
	DeleteGroup        ChangeKind = "delete_group"
	PrepareDeleteGroup ChangeKind = "prepare_delete_group"
	PrepareRecycleBin  ChangeKind = "prepare_recycle_bin"
	AddAttachment      ChangeKind = "add_attachment"
	ReplaceAttachment  ChangeKind = "replace_attachment"
	RemoveAttachment   ChangeKind = "remove_attachment"
)

type Change struct {
	Kind            ChangeKind
	EntryUUID       string
	GroupUUID       string
	Values          map[string]string
	Password        []byte
	PasswordChanged bool
	NewName         string
	AttachmentName  string
	AttachmentData  []byte
	AllowDataLoss   bool
	DataLossConsent [32]byte
}

// DataLossError describes metadata that this backend cannot preserve on write.
// A caller must obtain consent for the current database revision before retrying.
type DataLossError struct {
	Fields    []string
	Path      string
	Revision  [32]byte
	Operation [32]byte
}

func (e *DataLossError) Error() string { return "vault metadata loss requires confirmation" }

type Writer interface {
	Apply(context.Context, *Database, Credentials, Change, string) (*Database, error)
}

type Database struct {
	native             *nativeState
	nativeOwner        bool
	Path               string
	Name               string
	RootUUID           string
	RecycleBinUUID     string
	RecycleBinEnabled  bool
	RecycleBinVerified bool
	GroupDeletePlan    *GroupDeletePlan
	Groups             []Group
	Entries            []Entry
	Revision           [32]byte
}

// GroupDeletePlan describes the effects confirmed after the native bin probe.
type GroupDeletePlan struct {
	GroupUUID       string
	GroupName       string
	TargetEntries   int
	TargetGroups    int
	MatchingGroups  int
	MatchingEntries int
	BinPath         string
	Revision        [32]byte
}

func (d *Database) Clone() *Database {
	if d == nil {
		return nil
	}
	out := *d
	out.nativeOwner = false
	if d.GroupDeletePlan != nil {
		plan := *d.GroupDeletePlan
		out.GroupDeletePlan = &plan
	}
	out.Groups = append([]Group(nil), d.Groups...)
	out.Entries = make([]Entry, len(d.Entries))
	for i, e := range d.Entries {
		out.Entries[i] = cloneEntry(e, true)
	}
	return &out
}

func cloneEntry(e Entry, includeHistory bool) Entry {
	out := e
	out.Fields = append([]Field(nil), e.Fields...)
	out.Tags = append([]string(nil), e.Tags...)
	out.AttachmentNames = append([]string(nil), e.AttachmentNames...)
	out.History = nil
	if includeHistory && len(e.History) > 0 {
		out.History = make([]Entry, len(e.History))
		for i, snapshot := range e.History {
			out.History[i] = cloneEntry(snapshot, false)
		}
	}
	if !includeHistory {
		out.HistoryCount = 0
		out.History = nil
	}
	return out
}

func canonicalPath(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return "", err
	}
	return resolved, nil
}

func Revision(path string) ([32]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return [32]byte{}, ErrInvalidPath
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [32]byte{}, ErrInvalidPath
	}
	var sum [32]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

// Group, Entry, and Field are shared with test fixture readers.
type Group = vaultmodel.Group
type Entry = vaultmodel.Entry
type Field = vaultmodel.Field

// SharesBackend reports whether two models refer to the same unlocked database.
func (d *Database) SharesBackend(other *Database) bool {
	return d != nil && other != nil && d.native != nil && d.native == other.native
}

// TransferBackend transfers lock ownership to a successful replacement.
func (d *Database) TransferBackend(other *Database) {
	if d == nil || other == nil || d.native == nil || d.native != other.native {
		return
	}
	state := d.native
	state.mu.Lock()
	defer state.mu.Unlock()
	if d.native == state && other.native == state && state.db != nil {
		d.nativeOwner = false
		other.nativeOwner = true
	}
}

// BorrowBackendOwnership marks an update as borrowed while the workspace session
// retains ownership of the unlocked data. Callers
// must use this only on private results, never on a live workspace database.
func (d *Database) BorrowBackendOwnership() {
	if d == nil || d.native == nil {
		return
	}
	state := d.native
	state.mu.Lock()
	d.nativeOwner = false
	state.mu.Unlock()
}

// Unlocked reports whether this model has an unlocked backing database.
func (d *Database) Unlocked() bool { return d != nil && d.native != nil }

// Clear drops this model's references and wipes the unlocked backend when it
// owns that backend. Go strings are immutable, so their bytes cannot be wiped.
func (d *Database) Clear() {
	if d == nil {
		return
	}
	if d.native != nil {
		d.native.clearIfOwner(d)
	}
	d.GroupDeletePlan = nil
	for i := range d.Entries {
		clearEntry(&d.Entries[i])
	}
	d.Entries = nil
	d.Groups = nil
}

func clearEntry(e *Entry) {
	if e == nil {
		return
	}
	e.DetailsLoaded = false
	for j := range e.Fields {
		e.Fields[j].Value = ""
	}
	e.Fields = nil
	e.Tags = nil
	e.AttachmentNames = nil
	e.HistoryCount = 0
	e.HasTOTP = false
	e.TOTPPeriodSeconds = 0
	for i := range e.History {
		clearEntry(&e.History[i])
	}
	e.History = nil
}
