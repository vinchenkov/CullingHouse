//go:build !darwin

package boundary

import "io/fs"

// ADR-017's native ACL leg is macOS-specific. Non-Darwin builds retain the
// existing owner and group/other mode predicate.
func trustedNoGrantingACL(string, fs.FileInfo, int) error { return nil }
