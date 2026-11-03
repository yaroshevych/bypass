package xmlparse

import (
	"bypass/internal/vaultmodel"
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type xmlNode struct {
	XMLName xml.Name
	Attr    []xml.Attr `xml:",any,attr"`
	Text    string     `xml:",chardata"`
	Nodes   []xmlNode  `xml:",any"`
}

func child(n xmlNode, name string) (xmlNode, bool) {
	for _, c := range n.Nodes {
		if c.XMLName.Local == name {
			return c, true
		}
	}
	return xmlNode{}, false
}

func children(n xmlNode, name string) []xmlNode {
	var out []xmlNode
	for _, c := range n.Nodes {
		if c.XMLName.Local == name {
			out = append(out, c)
		}
	}
	return out
}

func value(n xmlNode, name string) string { return strings.TrimSpace(rawValue(n, name)) }

func rawValue(n xmlNode, name string) string { c, _ := child(n, name); return c.Text }

func attr(n xmlNode, name string) string {
	for _, a := range n.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

type Group = vaultmodel.Group
type Entry = vaultmodel.Entry
type Field = vaultmodel.Field

type Database struct {
	Path, Name, RootUUID, RecycleBinUUID string
	RecycleBinEnabled                    bool
	Groups                               []Group
	Entries                              []Entry
}

var ErrMalformed = errors.New("malformed vault export")

// Decode parses a native KeePassXC XML export for independent test readback.
func Decode(data []byte, path string) (*Database, error) {
	d := xml.NewDecoder(bytes.NewReader(data))
	d.Strict = true
	var root xmlNode
	if err := d.Decode(&root); err != nil {
		return nil, fmt.Errorf("%w: invalid XML", ErrMalformed)
	}
	var extra xmlNode
	if err := d.Decode(&extra); err != io.EOF {
		return nil, fmt.Errorf("%w: trailing XML data", ErrMalformed)
	}
	if root.XMLName.Local != "KeePassFile" {
		return nil, fmt.Errorf("%w: unexpected document root", ErrMalformed)
	}
	meta, ok := child(root, "Meta")
	if !ok {
		return nil, fmt.Errorf("%w: missing metadata", ErrMalformed)
	}
	rootNode, ok := child(root, "Root")
	if !ok {
		return nil, fmt.Errorf("%w: missing root", ErrMalformed)
	}
	rootGroup, ok := child(rootNode, "Group")
	if !ok {
		return nil, fmt.Errorf("%w: missing root group", ErrMalformed)
	}
	db := &Database{Path: path, Name: rawValue(meta, "DatabaseName"), RecycleBinEnabled: strings.EqualFold(value(meta, "RecycleBinEnabled"), "true")}
	db.RecycleBinUUID = vaultmodel.NormalizeUUID(value(meta, "RecycleBinUUID"))
	if raw := strings.TrimSpace(value(meta, "RecycleBinUUID")); raw != "" && db.RecycleBinUUID == "" {
		return nil, fmt.Errorf("%w: invalid recycle bin UUID", ErrMalformed)
	}
	db.RootUUID = vaultmodel.NormalizeUUID(value(rootGroup, "UUID"))
	defaultPasswordProtection := true
	if mp, ok := child(meta, "MemoryProtection"); ok {
		if raw, present := child(mp, "ProtectPassword"); present {
			defaultPasswordProtection = !strings.EqualFold(strings.TrimSpace(raw.Text), "false")
		}
	}
	walkGroup(db, rootGroup, "", "", false, true, db.RecycleBinUUID, defaultPasswordProtection)
	seenUUIDs := make(map[string]struct{}, len(db.Groups)+len(db.Entries))
	for _, g := range db.Groups {
		if !validEntityUUID(g.UUID) {
			return nil, fmt.Errorf("%w: group has missing or invalid UUID", ErrMalformed)
		}
		if _, exists := seenUUIDs[g.UUID]; exists {
			return nil, fmt.Errorf("%w: duplicate UUID", ErrMalformed)
		}
		seenUUIDs[g.UUID] = struct{}{}
	}
	for _, e := range db.Entries {
		if !validEntityUUID(e.UUID) {
			return nil, fmt.Errorf("%w: entry has missing or invalid UUID", ErrMalformed)
		}
		if _, exists := seenUUIDs[e.UUID]; exists {
			return nil, fmt.Errorf("%w: duplicate UUID", ErrMalformed)
		}
		seenUUIDs[e.UUID] = struct{}{}
	}
	return db, nil
}

func walkGroup(db *Database, n xmlNode, parentUUID, parentPath string, parentRecycled, inheritedSearch bool, recycleUUID string, defaultPasswordProtection bool) {
	uuid := vaultmodel.NormalizeUUID(value(n, "UUID"))
	name := rawValue(n, "Name")
	path := name
	if parentPath != "" {
		path = parentPath + "/" + name
	}
	recycled := parentRecycled || (recycleUUID != "" && uuid == recycleUUID)
	search := inheritedSearch
	if raw := strings.ToLower(strings.TrimSpace(value(n, "EnableSearching"))); raw == "true" {
		search = true
	} else if raw == "false" {
		search = false
	}
	db.Groups = append(db.Groups, Group{UUID: uuid, ParentUUID: parentUUID, Name: name, Path: path, Recycled: recycled, SearchEnabled: search})
	for _, e := range children(n, "Entry") {
		db.Entries = append(db.Entries, parseEntry(e, uuid, path, recycled, defaultPasswordProtection))
	}
	for _, g := range children(n, "Group") {
		walkGroup(db, g, uuid, path, recycled, search, recycleUUID, defaultPasswordProtection)
	}
}

func parseEntry(n xmlNode, groupUUID, groupPath string, recycled, defaultPasswordProtection bool) Entry {
	return parseEntryWithHistory(n, groupUUID, groupPath, recycled, defaultPasswordProtection, true)
}

func parseEntryWithHistory(n xmlNode, groupUUID, groupPath string, recycled, defaultPasswordProtection, includeHistory bool) Entry {
	e := Entry{UUID: vaultmodel.NormalizeUUID(value(n, "UUID")), GroupUUID: groupUUID, GroupPath: groupPath, Recycled: recycled}
	t := firstChild(n, "Times")
	if t != nil {
		e.Created = parseTime(value(*t, "CreationTime"))
		e.Modified = parseTime(value(*t, "LastModificationTime"))
		e.Expires = parseTime(value(*t, "ExpiryTime"))
		e.ExpiryEnabled = strings.EqualFold(value(*t, "Expires"), "true")
	}
	e.Tags = vaultmodel.SplitTags(rawValue(n, "Tags"))
	for _, s := range children(n, "String") {
		key := rawValue(s, "Key")
		v, ok := child(s, "Value")
		if !ok {
			continue
		}
		protected := strings.EqualFold(attr(v, "Protected"), "true") || strings.EqualFold(attr(v, "ProtectInMemory"), "true") || isTOTPSecretField(key)
		// KeePassXC's XML exporter normally marks protected values. Password is
		// protected by the database default when older exports omit the marker.
		if key == "Password" && attr(v, "Protected") == "" && attr(v, "ProtectInMemory") == "" {
			protected = defaultPasswordProtection
		}
		e.Fields = append(e.Fields, Field{Name: key, Value: v.Text, Protected: protected})
	}
	for _, b := range children(n, "Binary") {
		if k := rawValue(b, "Key"); k != "" {
			e.AttachmentNames = append(e.AttachmentNames, k)
		}
	}
	if h, ok := child(n, "History"); ok && includeHistory {
		snapshots := children(h, "Entry")
		e.HistoryCount = len(snapshots)
		if len(snapshots) > 0 {
			e.History = make([]Entry, 0, len(snapshots))
			for _, snapshot := range snapshots {
				e.History = append(e.History, parseEntryWithHistory(snapshot, groupUUID, groupPath, recycled, defaultPasswordProtection, false))
			}
		}
	}
	e.HasTOTP, e.TOTPPeriodSeconds = totpMetadata(e.Fields)
	return e
}

func isTOTPSecretField(name string) bool {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "otp", "totp seed",
		"timeotp-secret-base32", "timeotp-secret-hex", "timeotp-secret-base64", "timeotp-secret-utf8",
		"keepass2timeotp-secret-base32", "keepass2timeotp-secret-hex", "keepass2timeotp-secret-base64", "keepass2timeotp-secret-utf8":
		return true
	default:
		return false
	}
}

func firstChild(n xmlNode, name string) *xmlNode {
	c, ok := child(n, name)
	if !ok {
		return nil
	}
	return &c
}

func validEntityUUID(uuid string) bool {
	compact := strings.ReplaceAll(uuid, "-", "")
	if len(compact) != 32 {
		return false
	}
	b, err := hex.DecodeString(compact)
	if err != nil || len(b) != 16 {
		return false
	}
	for _, v := range b {
		if v != 0 {
			return true
		}
	}
	return false
}

func parseTime(raw string) time.Time {
	if raw == "" {
		return time.Time{}
	}
	if t, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return t
	}
	b, err := base64.StdEncoding.DecodeString(raw)
	if err == nil && len(b) >= 8 {
		secs := binary.LittleEndian.Uint64(b[:8])
		// KDBX stores seconds from 0001-01-01 UTC.
		const unixOffset = uint64(62135596800)
		if secs <= uint64(1<<63-1) {
			return time.Unix(int64(secs)-int64(unixOffset), 0).UTC()
		}
	}
	return time.Time{}
}

func totpMetadata(fields []Field) (bool, int) {
	values := make(map[string]string, len(fields))
	for _, f := range fields {
		values[strings.ToLower(strings.TrimSpace(f.Name))] = strings.TrimSpace(f.Value)
	}
	secret := ""
	for _, key := range []string{
		"totp seed", "timeotp-secret-base32", "timeotp-secret-hex", "timeotp-secret-base64", "timeotp-secret-utf8",
		"keepass2timeotp-secret-base32", "keepass2timeotp-secret-hex", "keepass2timeotp-secret-base64", "keepass2timeotp-secret-utf8",
	} {
		if values[key] != "" {
			secret = values[key]
			break
		}
	}
	period := 0
	for _, key := range []string{"timeotp-period", "timeotp-step", "keepass2timeotp-step"} {
		if period = parsePositivePeriod(values[key]); period > 0 {
			break
		}
	}
	settings := values["totp settings"]
	if settings != "" {
		if parts := strings.Split(settings, ";"); len(parts) > 0 && period == 0 {
			period = parsePositivePeriod(parts[0])
		}
	}
	otp := values["otp"]
	if otp != "" {
		if parsed, err := url.Parse(otp); err == nil {
			query := parsed.Query()
			if secret == "" {
				secret = query.Get("secret")
				if secret == "" {
					secret = query.Get("key")
				}
			}
			if period == 0 {
				period = parsePositivePeriod(query.Get("period"))
				if period == 0 {
					period = parsePositivePeriod(query.Get("step"))
				}
			}
		}
		if secret == "" && strings.Contains(otp, "=") {
			if query, err := url.ParseQuery(otp); err == nil {
				secret = query.Get("secret")
				if secret == "" {
					secret = query.Get("key")
				}
				if period == 0 {
					period = parsePositivePeriod(query.Get("period"))
					if period == 0 {
						period = parsePositivePeriod(query.Get("step"))
					}
				}
			}
		}
	}
	hasTOTP := secret != "" || otp != ""
	if !hasTOTP {
		return false, 0
	}
	if period == 0 {
		period = 30
	}
	return true, period
}

func parsePositivePeriod(raw string) int {
	n, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil || n < 1 || n > 86400 {
		return 0
	}
	return n
}
