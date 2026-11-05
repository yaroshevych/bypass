package vaultmodel

import "time"

type Group struct {
	UUID          string
	ParentUUID    string
	Name          string
	Path          string
	Recycled      bool
	SearchEnabled bool
}

type Entry struct {
	OpaqueGroup       bool
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
