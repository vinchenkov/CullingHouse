// Package boundary owns the pure, fail-closed configuration and confinement
// policy shared by profile admission and dispatch planning.
package boundary

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/BurntSushi/toml"
)

const (
	maxAllowlistBytes    = 256 * 1024
	maxAllowEntries      = 256
	maxAllowPathBytes    = 4096
	maxTargetBytes       = 1024
	maxTargetComponent   = 255
	requiredAllowVersion = int64(1)
)

// Access is a requested or authorized bind-mount mode.
type Access string

const (
	AccessRO Access = "ro"
	AccessRW Access = "rw"
)

// AllowEntry is one operator-owned authorization root and its stable
// container target. Maximum is an upper bound, not a requested mode.
type AllowEntry struct {
	Path    string
	Target  string
	Maximum Access
}

// MountAllowlist is an immutable, validated mount policy. Construct it with
// ParseMountAllowlist; Entries returns a defensive copy.
type MountAllowlist struct {
	entries []AllowEntry
}

// Entries returns the validated entries in source order.
func (a MountAllowlist) Entries() []AllowEntry {
	return append([]AllowEntry(nil), a.entries...)
}

type allowlistDocument struct {
	Version int64                `toml:"version"`
	Allow   []allowlistEntryTOML `toml:"allow"`
}

type allowlistEntryTOML struct {
	Path   string `toml:"path"`
	Target string `toml:"target"`
	Access string `toml:"access"`
}

// ParseMountAllowlist parses the exact ADR-017 TOML v1 schema. It performs no
// filesystem I/O; owner/mode, identity, overlap, and symlink checks belong to
// the identity-validation layer.
func ParseMountAllowlist(data []byte) (MountAllowlist, error) {
	if len(data) > maxAllowlistBytes {
		return MountAllowlist{}, fmt.Errorf("mount-allowlist: %d bytes exceeds %d-byte limit", len(data), maxAllowlistBytes)
	}
	if !utf8.Valid(data) {
		return MountAllowlist{}, fmt.Errorf("mount-allowlist: input is not valid UTF-8")
	}

	var document allowlistDocument
	metadata, err := toml.NewDecoder(bytes.NewReader(data)).Decode(&document)
	if err != nil {
		return MountAllowlist{}, fmt.Errorf("mount-allowlist: invalid TOML: %w", err)
	}
	if err := validateAllowlistShape(&metadata); err != nil {
		return MountAllowlist{}, err
	}
	if document.Version != requiredAllowVersion {
		return MountAllowlist{}, fmt.Errorf("mount-allowlist: version must be integer %d", requiredAllowVersion)
	}
	if len(document.Allow) > maxAllowEntries {
		return MountAllowlist{}, fmt.Errorf("mount-allowlist: %d allow entries exceeds %d-entry limit", len(document.Allow), maxAllowEntries)
	}

	entries := make([]AllowEntry, len(document.Allow))
	for i, raw := range document.Allow {
		entry, err := validateAllowEntry(i, raw)
		if err != nil {
			return MountAllowlist{}, err
		}
		entries[i] = entry
	}
	if err := validateTargetSet(entries); err != nil {
		return MountAllowlist{}, err
	}
	return MountAllowlist{entries: entries}, nil
}

func validateAllowlistShape(metadata *toml.MetaData) error {
	if !metadata.IsDefined("version") || metadata.Type("version") != "Integer" {
		return fmt.Errorf("mount-allowlist: version must appear once as an integer")
	}
	if metadata.IsDefined("allow") && metadata.Type("allow") != "ArrayHash" {
		return fmt.Errorf("mount-allowlist: allow entries must use [[allow]] array tables")
	}

	for _, key := range metadata.Keys() {
		valid := len(key) == 1 && (key[0] == "version" || key[0] == "allow")
		if len(key) == 2 && key[0] == "allow" {
			valid = key[1] == "path" || key[1] == "target" || key[1] == "access"
		}
		if !valid {
			return fmt.Errorf("mount-allowlist: unknown or misplaced key %q", key.String())
		}
	}
	if undecoded := metadata.Undecoded(); len(undecoded) != 0 {
		return fmt.Errorf("mount-allowlist: undecoded key %q", undecoded[0].String())
	}
	return nil
}

func validateAllowEntry(index int, raw allowlistEntryTOML) (AllowEntry, error) {
	label := fmt.Sprintf("mount-allowlist allow[%d]", index)
	if raw.Path == "" {
		return AllowEntry{}, fmt.Errorf("%s: path is required", label)
	}
	if !utf8.ValidString(raw.Path) {
		return AllowEntry{}, fmt.Errorf("%s: path is not valid UTF-8", label)
	}
	if len(raw.Path) > maxAllowPathBytes {
		return AllowEntry{}, fmt.Errorf("%s: path exceeds %d bytes", label, maxAllowPathBytes)
	}
	if !filepath.IsAbs(raw.Path) {
		return AllowEntry{}, fmt.Errorf("%s: path must be absolute", label)
	}
	if strings.ContainsAny(raw.Path, "\x00\r\n") {
		return AllowEntry{}, fmt.Errorf("%s: path contains NUL or newline", label)
	}
	if err := ValidateTarget(raw.Target); err != nil {
		return AllowEntry{}, fmt.Errorf("%s: target: %w", label, err)
	}
	maximum := Access(raw.Access)
	if !validAccess(maximum) {
		return AllowEntry{}, fmt.Errorf("%s: access must be exactly %q or %q", label, AccessRO, AccessRW)
	}
	return AllowEntry{Path: raw.Path, Target: raw.Target, Maximum: maximum}, nil
}

// ValidateTarget checks the stable relative POSIX target grammar without
// cleaning or rewriting the supplied spelling.
func ValidateTarget(target string) error {
	if target == "" {
		return fmt.Errorf("must not be empty")
	}
	if !utf8.ValidString(target) {
		return fmt.Errorf("must be valid UTF-8")
	}
	if len(target) > maxTargetBytes {
		return fmt.Errorf("exceeds %d bytes", maxTargetBytes)
	}
	if path.IsAbs(target) || path.Clean(target) != target {
		return fmt.Errorf("must be relative and already POSIX-clean")
	}

	for _, component := range strings.Split(target, "/") {
		if component == "" || component == "." || component == ".." {
			return fmt.Errorf("contains an empty, dot, or dot-dot component")
		}
		if len(component) > maxTargetComponent {
			return fmt.Errorf("component exceeds %d bytes", maxTargetComponent)
		}
		for i := 0; i < len(component); i++ {
			b := component[i]
			if b == ':' || b == '\\' || b == 0 || b < 0x20 || b == 0x7f {
				return fmt.Errorf("component contains colon, backslash, NUL, or ASCII control")
			}
		}
	}
	return nil
}

func validateTargetSet(entries []AllowEntry) error {
	for i := range entries {
		for j := 0; j < i; j++ {
			first, second := entries[j].Target, entries[i].Target
			if first == second || strings.HasPrefix(first, second+"/") || strings.HasPrefix(second, first+"/") {
				return fmt.Errorf("mount-allowlist: targets %q and %q are equal or ancestor-overlapping", first, second)
			}
		}
	}
	return nil
}

// ResolveAccess applies ADR-017's bilateral access table. An unauthorized RW
// request is rejected; it is never silently downgraded to RO.
func ResolveAccess(requested, maximum Access) (Access, error) {
	if !validAccess(requested) {
		return "", fmt.Errorf("requested access must be exactly %q or %q", AccessRO, AccessRW)
	}
	if !validAccess(maximum) {
		return "", fmt.Errorf("maximum access must be exactly %q or %q", AccessRO, AccessRW)
	}
	if requested == AccessRW && maximum == AccessRO {
		return "", fmt.Errorf("requested RW access exceeds RO allowlist maximum")
	}
	return requested, nil
}

func validAccess(access Access) bool {
	return access == AccessRO || access == AccessRW
}
