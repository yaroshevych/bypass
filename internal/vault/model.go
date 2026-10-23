package vault

import (
	"context"
	"time"
)

type Credentials struct {
	Password   []byte
	KeyFile    string
	NoPassword bool
	YubiKey    string
}

type Backend interface {
	Open(context.Context, string, Credentials) (*Database, error)
}

type Database struct {
	Path              string
	Name              string
	RootUUID          string
	RecycleBinUUID    string
	RecycleBinEnabled bool
	Groups            []Group
	Entries           []Entry
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
	UUID            string
	GroupUUID       string
	GroupPath       string
	Fields          []Field
	Tags            []string
	Recycled        bool
	Created         time.Time
	Modified        time.Time
	Expires         time.Time
	ExpiryEnabled   bool
	AttachmentNames []string
	HistoryCount    int
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

// Clear removes references to field values and drops the database's slices.
// Go strings are immutable, so this cannot reliably overwrite their backing bytes.
func (d *Database) Clear() {
	if d == nil {
		return
	}
	for i := range d.Entries {
		for j := range d.Entries[i].Fields {
			d.Entries[i].Fields[j].Value = ""
		}
		d.Entries[i].Fields = nil
		d.Entries[i].Tags = nil
		d.Entries[i].AttachmentNames = nil
	}
	d.Entries = nil
	d.Groups = nil
}
