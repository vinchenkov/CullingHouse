package boundary

import (
	"golang.org/x/text/cases"
	"golang.org/x/text/unicode/norm"
)

// suffixCaseMode is the case rule of the volume containing an absent root's
// nearest existing canonical ancestor. D8 applies it only to unresolved suffix
// components; the ancestor half remains filesystem identity.
type suffixCaseMode uint8

const (
	suffixCaseUnknown suffixCaseMode = iota
	suffixCaseSensitive
	suffixCaseInsensitive
)

// suffixesOverlap reports whether either suffix is a whole-component prefix of
// the other. That is the absent-root form of bidirectional intersection: the
// source may be an ancestor or a descendant of the would-be protected path.
func suffixesOverlap(protected, source []string, mode suffixCaseMode) (bool, error) {
	limit := min(len(protected), len(source))
	for i := 0; i < limit; i++ {
		equal, err := suffixComponentEqual(protected[i], source[i], mode)
		if err != nil {
			return false, err
		}
		if !equal {
			return false, nil
		}
	}
	return true, nil
}

func suffixComponentEqual(a, b string, mode suffixCaseMode) (bool, error) {
	a = norm.NFC.String(a)
	b = norm.NFC.String(b)
	if a == b {
		return true, nil
	}

	// cases.Fold performs full Unicode folding, including multi-rune expansions
	// such as ß -> ss. EqualFold implements simple folding and is insufficient.
	fold := cases.Fold()
	aFolded := norm.NFC.String(fold.String(a))
	bFolded := norm.NFC.String(fold.String(b))
	if aFolded != bFolded {
		return false, nil // distinct under either volume mode
	}

	switch mode {
	case suffixCaseSensitive:
		return false, nil
	case suffixCaseInsensitive:
		return true, nil
	default:
		return false, mountErrf(CodeDeniedRoot,
			"volume case sensitivity is unknown for fold-equivalent suffix components %q and %q; ambiguity denies",
			a, b)
	}
}
