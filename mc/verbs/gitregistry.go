package verbs

// The authoritative Git control/projection registry (ADR-017 D5's "every
// registered real Git control directory"; ADR-021's Own/OtherGitControls
// input). Registration is live host-side resolution against the registered
// workspace root at every attest: ADR-021 D9/D11 forbid caching jurisdiction
// inputs, the real Git layout is the only truth a stored table could try to
// mirror, and Darwin never invokes an operator-installed host Git (ADR-016
// D5) — so the registry reads the two bounded pointer files itself and
// resolves filesystem identities, nothing more.

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"mc/boundary"
)

// maxGitPointerBytes bounds the two administrative pointer files this
// registry ever reads (`.git` gitdir pointers and `commondir`). Real
// pointers are one short line; anything larger is not a recognizable Git
// administrative shape and refuses rather than being scanned.
const maxGitPointerBytes = 4096

// resolveWorksourceGitControls resolves one registered Worksource's Git
// administrative identities. The returned set feeds ADR-021's protected set,
// so absence is still membership (D8): a missing `.git` is returned
// absent-encoded, while an unreadable, symlinked, unparsable, or dangling
// administrative shape is ambiguity and denies with a typed MountError.
func resolveWorksourceGitControls(workspaceRoot string) ([]boundary.ProtectedID, error) {
	if workspaceRoot == "" {
		// No declared root means there is nothing to register; the Worksource
		// cannot reach any filesystem grant without a profile either.
		return []boundary.ProtectedID{}, nil
	}
	gitPath := filepath.Join(workspaceRoot, ".git")
	info, err := os.Lstat(gitPath)
	if os.IsNotExist(err) {
		// A bare repository has no .git child: its registered workspace root
		// is itself the administrative identity. Protect the root whenever any
		// closed bare-control marker is present; treating it as an ordinary
		// non-Git workspace would expose objects/refs/config through an
		// allowlisted descendant. Partial or malformed bare shapes are still
		// protected here and will fail closed when a later projection/setup
		// slice tries to consume them.
		bare, err := workspaceHasBareGitMarker(workspaceRoot)
		if err != nil {
			return nil, err
		}
		if bare {
			root, err := resolveDispatchProtected(workspaceRoot, false)
			if err != nil {
				return nil, err
			}
			if !root.IsDir {
				return nil, &boundary.MountError{
					Code: boundary.CodeSourceWrongKind,
					Msg:  "registered bare Git control root is not a directory",
				}
			}
			return []boundary.ProtectedID{root}, nil
		}
		return []boundary.ProtectedID{{Canonical: filepath.Clean(gitPath)}}, nil
	}
	if err != nil {
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "registered workspace Git control is unreadable: " + err.Error(),
		}
	}

	switch {
	case info.Mode()&os.ModeSymlink != 0:
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "registered workspace .git is a symlink; a control identity must be resolved, not aliased",
		}
	case info.IsDir():
		id, err := resolveDispatchProtected(gitPath, false)
		if err != nil {
			return nil, err
		}
		return []boundary.ProtectedID{id}, nil
	case info.Mode().IsRegular():
		return resolveGitPointerControls(workspaceRoot, gitPath)
	default:
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "registered workspace .git is neither a directory nor a gitdir pointer file",
		}
	}
}

func workspaceHasBareGitMarker(workspaceRoot string) (bool, error) {
	for _, name := range []string{"HEAD", "objects", "refs"} {
		_, err := os.Lstat(filepath.Join(workspaceRoot, name))
		switch {
		case err == nil:
			return true, nil
		case os.IsNotExist(err):
			continue
		default:
			return false, &boundary.MountError{
				Code: boundary.CodeSourceWrongKind,
				Msg:  "registered bare Git control marker is unreadable: " + err.Error(),
			}
		}
	}
	return false, nil
}

// resolveGitPointerControls chases a linked-worktree checkout: the `.git`
// pointer file itself, the administrative gitdir it names, and — when that
// gitdir carries a `commondir` file — the shared control root. An identity
// outside the workspace is still registered: it is a real control whatever
// directory it lives in.
func resolveGitPointerControls(workspaceRoot, gitPath string) ([]boundary.ProtectedID, error) {
	target, err := readGitPointerLine(gitPath, "gitdir: ")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(target) {
		target = filepath.Join(workspaceRoot, target)
	}

	pointer, err := resolveDispatchProtected(gitPath, false)
	if err != nil {
		return nil, err
	}
	gitdir, err := resolveDispatchProtected(target, false)
	if err != nil {
		return nil, err
	}
	if !gitdir.IsDir {
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "gitdir pointer names a non-directory administrative identity",
		}
	}
	controls := []boundary.ProtectedID{pointer, gitdir}

	commonPath := filepath.Join(gitdir.Canonical, "commondir")
	if _, err := os.Lstat(commonPath); os.IsNotExist(err) {
		return dedupSortedControls(controls), nil
	} else if err != nil {
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "administrative commondir is unreadable: " + err.Error(),
		}
	}
	commonTarget, err := readGitPointerLine(commonPath, "")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(commonTarget) {
		commonTarget = filepath.Join(gitdir.Canonical, commonTarget)
	}
	common, err := resolveDispatchProtected(commonTarget, false)
	if err != nil {
		return nil, err
	}
	if !common.IsDir {
		return nil, &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "commondir names a non-directory control root",
		}
	}
	return dedupSortedControls(append(controls, common)), nil
}

// readGitPointerLine reads one bounded administrative pointer file: exactly
// one non-empty line, an optional trailing newline, and — when prefix is
// nonempty — that exact prefix. Anything else is not a recognizable Git
// administrative shape.
func readGitPointerLine(path, prefix string) (string, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() > maxGitPointerBytes {
		return "", &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "administrative pointer file is absent, aliased, or exceeds its byte bound",
		}
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return "", &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "administrative pointer file is unreadable: " + err.Error(),
		}
	}
	line, ok := strings.CutSuffix(string(body), "\n")
	if !ok {
		line = string(body)
	}
	value, ok := strings.CutPrefix(line, prefix)
	if !ok || value == "" || strings.ContainsAny(value, "\n\x00") {
		return "", &boundary.MountError{
			Code: boundary.CodeSourceWrongKind,
			Msg:  "administrative pointer file does not carry exactly one pointer line",
		}
	}
	return value, nil
}

func dedupSortedControls(controls []boundary.ProtectedID) []boundary.ProtectedID {
	sort.Slice(controls, func(i, j int) bool { return controls[i].Canonical < controls[j].Canonical })
	out := controls[:0]
	for i, id := range controls {
		if i > 0 && id.Canonical == controls[i-1].Canonical {
			continue
		}
		out = append(out, id)
	}
	return out
}
