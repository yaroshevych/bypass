package vault

import (
	"context"
	"crypto/sha256"
	"io"
	"os"
	"path/filepath"
	"time"
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
	AddEntry          ChangeKind = "add_entry"
	EditEntry         ChangeKind = "edit_entry"
	MoveEntry         ChangeKind = "move_entry"
	DeleteEntry       ChangeKind = "delete_entry"
	CreateGroup       ChangeKind = "create_group"
	DeleteGroup       ChangeKind = "delete_group"
	AddAttachment     ChangeKind = "add_attachment"
	ReplaceAttachment ChangeKind = "replace_attachment"
	RemoveAttachment  ChangeKind = "remove_attachment"
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
}

type Writer interface {
	Apply(context.Context, *Database, Credentials, Change, string) (*Database, error)
}

type Database struct {
	interactive       *interactiveSession
	interactiveOwner  bool
	Path              string
	Name              string
	RootUUID          string
	RecycleBinUUID    string
	RecycleBinEnabled bool
	Groups            []Group
	Entries           []Entry
	Revision          [32]byte
}

func (d *Database) Clone() *Database {
	if d == nil {
		return nil
	}
	out := *d
	out.interactiveOwner = false
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

type Group struct {
	UUID          string
	ParentUUID    string
	Name          string
	Path          string
	Recycled      bool
	SearchEnabled bool
}

type Entry struct {
	DetailsLoaded     bool
	UUID              string
	GroupUUID         string
	GroupPath         string
	Fields            []Field
	Tags              []string
	Recycled          bool
	Created           time.Time
	Modified          time.Time
	Expires           time.Time
	ExpiryEnabled     bool
	AttachmentNames   []string
	HistoryCount      int
	History           []Entry
	HasTOTP           bool
	TOTPPeriodSeconds int
}

type Field struct {
	Name      string
	Value     string
	Protected bool
}

func (e Entry) Get(name string) string {
	for _, f := range e.Fields {
		if f.Name == name {
			return f.Value
		}
	}
	return ""
}

func (e Entry) IsProtected(name string) bool {
	for _, f := range e.Fields {
		if f.Name == name {
			return f.Protected
		}
	}
	return false
}

// SharesInteractive reports whether two models use the same authenticated child.
func (d *Database) SharesInteractive(other *Database) bool {
	return d != nil && other != nil && d.interactive != nil && d.interactive == other.interactive
}

// TransferInteractive transfers process ownership to a successful replacement.
func (d *Database) TransferInteractive(other *Database) {
	if d.SharesInteractive(other) {
		d.interactiveOwner = false
		other.interactiveOwner = true
	}
}

// Interactive reports whether this database uses a live, credential-free CLI session.
func (d *Database) Interactive() bool { return d != nil && d.interactive != nil }

// Clear drops field references and closes an owned interactive process.
// Go strings are immutable, so this cannot reliably overwrite their backing bytes.
func (d *Database) Clear() {
	if d == nil {
		return
	}
	if d.interactiveOwner {
		d.interactive.close()
	}
	d.interactive = nil
	d.interactiveOwner = false
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
