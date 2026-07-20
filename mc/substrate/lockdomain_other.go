//go:build !linux

package substrate

// guardLockDomainAt accepts on every non-Linux platform, and that is a SCOPE
// decision, not a weakening.
//
// There is no /proc to consult off Linux, so a real check is impossible — but
// it is also unnecessary, because on macOS/Windows a native host process
// cannot open the spine at all: the bytes live inside the container runtime's
// VM, in a different kernel, with no host path to the file (Inv. 24, spec
// §11.5). Host components reach the spine by invoking the real `mc` INSIDE the
// lock domain, and that in-container process is Linux, where the guard above
// runs for real.
//
// The one thing that opens a spine natively on darwin is the test suite, which
// builds throwaway spines in t.TempDir() on APFS. Making the guard refuse
// those would not protect anything — a temp file no container ever opens has
// no second kernel to race — it would only delete the fast lane.
//
// Do NOT "fix" this by adding an env-var escape hatch on the Linux side. The
// Linux path is exactly where a real spine is opened by more than one process,
// which is the only place the invariant can actually be violated.
func guardLockDomainAt(...string) error { return nil }

// resolveExistingAncestor is identity off Linux: nothing consumes its result.
func resolveExistingAncestor(dir string) string { return dir }
