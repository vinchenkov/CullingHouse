package verbs

// The fixed first-task setup closure writer: ADR-016 D5's post-claim
// population of the resident-precreated standalone-task skeleton. It builds
// only from the durable receipt-attested root, materializes exactly the
// closed D6 table's generated covers and relative Git controls plus the
// pinned reachable object closure, and then joins the receipt-plus-15-row
// inspection before the rows can enter an agent plan.
//
// The closure arrives as an already-extracted pack/index pair pinned by
// digest; Darwin invokes no host Git here. The setup-container extraction
// that produces it, the Run-recorded pin columns, and D5's exact
// retry-residue acceptance are later slices — until they land, the digest
// pin is caller-supplied and any residue in the skeleton refuses without
// cleanup (setup failure after a valid claim is infrastructure failure,
// never permission to guess).

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"syscall"

	"mc/boundary"
)

// FirstTaskClosureFile is one extracted pack-store file of the pinned
// reachable object closure.
type FirstTaskClosureFile struct {
	Name string
	Body []byte
}

// FirstTaskClosure is the pinned reachable closure a first-task setup may
// materialize: exactly one pack with its index, reproduced by Digest.
type FirstTaskClosure struct {
	Digest string
	Files  []FirstTaskClosureFile
}

var (
	closurePackName  = regexp.MustCompile(`^pack-([0-9a-f]{40}|[0-9a-f]{64})\.(pack|idx)$`)
	closureDigestHex = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// maxFirstTaskClosureBytes is the admission fence over supplied closure
// bytes, checked before any hashing or filesystem work.
const maxFirstTaskClosureBytes = 1 << 30

// firstTaskClosureDigest is the canonical closure identity: sha256 over the
// version header then every file in name order as name NUL decimal-length
// NUL body. Covering the names makes set equality follow from digest
// equality.
func firstTaskClosureDigest(files []FirstTaskClosureFile) string {
	sorted := make([]FirstTaskClosureFile, len(files))
	copy(sorted, files)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Name < sorted[j].Name })
	h := sha256.New()
	h.Write([]byte("mc-first-task-closure/v1\n"))
	for _, f := range sorted {
		h.Write([]byte(f.Name))
		h.Write([]byte{0})
		h.Write([]byte(strconv.Itoa(len(f.Body))))
		h.Write([]byte{0})
		h.Write(f.Body)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func validateFirstTaskClosure(closure FirstTaskClosure) error {
	if len(closure.Files) != 2 {
		return Domainf("task setup closure is not exactly one pack with its index")
	}
	var total int64
	stems := map[string]string{}
	for _, f := range closure.Files {
		m := closurePackName.FindStringSubmatch(f.Name)
		if m == nil {
			return Domainf("task setup closure file name is outside the pack-store grammar")
		}
		if len(f.Body) == 0 {
			return Domainf("task setup closure file is empty")
		}
		if _, dup := stems[m[2]]; dup {
			return Domainf("task setup closure repeats a pack-store role")
		}
		stems[m[2]] = m[1]
		total += int64(len(f.Body))
	}
	if total > maxFirstTaskClosureBytes {
		return Domainf("task setup closure exceeds its admission bound")
	}
	if stems["pack"] == "" || stems["idx"] == "" || stems["pack"] != stems["idx"] {
		return Domainf("task setup closure is not exactly one pack with its matching index")
	}
	if !closureDigestHex.MatchString(closure.Digest) {
		return Domainf("task setup closure digest is not a canonical sha256")
	}
	if firstTaskClosureDigest(closure.Files) != closure.Digest {
		return Domainf("task setup closure does not reproduce its pinned digest")
	}
	return nil
}

// requireEmptyResidentChild proves one precreated writable child is still the
// exact empty operator-owned directory the resident registered. Any residue
// refuses without cleanup: a half-written earlier attempt is infrastructure
// failure until D5's exact retry-residue acceptance lands.
func requireEmptyResidentChild(root FirstTaskSetupRoot, child string) error {
	path := filepath.Join(root.Canonical, child)
	info, err := os.Lstat(path)
	if err != nil {
		return Domainf("task setup skeleton child %s is unreadable: %v", child, err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Domainf("task setup skeleton child %s is not its non-symlink directory", child)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != root.Receipt.Root.OwnerUID {
		return Domainf("task setup skeleton child %s is not owned by the host operator", child)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return Domainf("task setup skeleton child %s is unreadable: %v", child, err)
	}
	if len(entries) != 0 {
		return Domainf("task setup skeleton child %s already holds residue; closure materialization never overwrites", child)
	}
	return nil
}

// materializeFirstTaskTable creates every generated D6 row beneath the
// attested root plus the closure files, children beneath the resident's
// writable source/ and git/ only, each create refusing pre-existing state.
// Final modes are applied after every byte is in place: 0444 generated
// covers and closure files, 0555 generated empty covers, 0755 interior
// directories.
func materializeFirstTaskTable(root FirstTaskSetupRoot, closure FirstTaskClosure) error {
	preexisting := map[string]bool{
		root.Canonical:                          true,
		filepath.Join(root.Canonical, "source"): true,
		filepath.Join(root.Canonical, "git"):    true,
	}
	interior := map[string]bool{}
	type finalMode struct {
		path string
		mode os.FileMode
	}
	var modes []finalMode

	ensureParents := func(path string) error {
		var chain []string
		for p := filepath.Dir(path); !preexisting[p]; p = filepath.Dir(p) {
			if p == "/" || p == "." {
				return Domainf("task setup closure row escapes the attested root")
			}
			chain = append(chain, p)
		}
		for i := len(chain) - 1; i >= 0; i-- {
			p := chain[i]
			if interior[p] {
				continue
			}
			if err := os.Mkdir(p, 0o700); err != nil {
				return Domainf("task setup closure could not create an interior directory: %v", err)
			}
			interior[p] = true
			modes = append(modes, finalMode{p, 0o755})
		}
		return nil
	}
	writeNew := func(path string, body []byte, mode os.FileMode) error {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err != nil {
			return Domainf("task setup closure could not create a generated file: %v", err)
		}
		if _, err := f.Write(body); err != nil {
			f.Close()
			return Domainf("task setup closure could not write a generated file: %v", err)
		}
		if err := f.Close(); err != nil {
			return Domainf("task setup closure could not write a generated file: %v", err)
		}
		modes = append(modes, finalMode{path, mode})
		return nil
	}

	for _, row := range taskPlanRows(root.Receipt.TaskID) {
		if row.Rel == "" || row.Rel == "source" || row.Rel == "git" {
			continue
		}
		path := filepath.Join(root.Canonical, filepath.FromSlash(row.Rel))
		if err := ensureParents(path); err != nil {
			return err
		}
		if row.IsDir {
			if err := os.Mkdir(path, 0o700); err != nil {
				return Domainf("task setup closure could not create row %s: %v", row.Rel, err)
			}
			preexisting[path] = true
			mode := os.FileMode(0o755)
			if row.MustBeEmptyDir {
				mode = 0o555
			}
			modes = append(modes, finalMode{path, mode})
			continue
		}
		if row.WantBytes == nil {
			return Domainf("task setup closure row %s has no generated content pin", row.Rel)
		}
		if err := writeNew(path, row.WantBytes, 0o444); err != nil {
			return err
		}
	}

	packDir := filepath.Join(root.Canonical, "git", "objects", "pack")
	for _, f := range closure.Files {
		if err := writeNew(filepath.Join(packDir, f.Name), f.Body, 0o444); err != nil {
			return err
		}
	}

	for _, m := range modes {
		if err := os.Chmod(m.path, m.mode); err != nil {
			return Domainf("task setup closure could not pin a generated mode: %v", err)
		}
	}
	return nil
}

// recheckLandedClosure proves the pack store holds exactly the pinned
// closure as landed bytes, not merely as the bytes that were supplied: the
// digest covers names and content, so digest equality over the re-read
// store is set equality.
func recheckLandedClosure(rootCanonical string, closure FirstTaskClosure) error {
	dir := filepath.Join(rootCanonical, "git", "objects", "pack")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return Domainf("task setup closure store is unreadable: %v", err)
	}
	if len(entries) != len(closure.Files) {
		return Domainf("task setup closure store does not hold exactly the pinned closure")
	}
	landed := make([]FirstTaskClosureFile, 0, len(entries))
	for _, e := range entries {
		if !e.Type().IsRegular() || e.Type()&fs.ModeSymlink != 0 {
			return Domainf("task setup closure store holds a non-regular entry")
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return Domainf("task setup closure store is unreadable: %v", err)
		}
		landed = append(landed, FirstTaskClosureFile{Name: e.Name(), Body: body})
	}
	if firstTaskClosureDigest(landed) != closure.Digest {
		return Domainf("task setup closure store does not reproduce its pinned digest")
	}
	return nil
}

// WriteFirstTaskSetupClosure is the fixed first-task setup closure writer.
// It re-attests the durable receipt's root, requires the exact empty
// resident skeleton, materializes only the pinned reachable closure and the
// generated covers/relative Git controls of the closed D6 table, proves the
// landed closure reproduces its digest, and returns only through the joined
// receipt-plus-15-row inspection. It has no production caller yet: the
// setup-container slice that extracts real closures will drive it.
func WriteFirstTaskSetupClosure(db *sql.DB, runID, workspaceRoot string, closure FirstTaskClosure) (FirstTaskSetupRoot, map[boundary.TypedKind]boundary.ProtectedID, error) {
	if err := validateFirstTaskClosure(closure); err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	root, err := AttestFirstTaskSetupRoot(db, runID, workspaceRoot)
	if err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	for _, child := range []string{"source", "git"} {
		if err := requireEmptyResidentChild(root, child); err != nil {
			return FirstTaskSetupRoot{}, nil, err
		}
	}
	if err := materializeFirstTaskTable(root, closure); err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	if err := recheckLandedClosure(root.Canonical, closure); err != nil {
		return FirstTaskSetupRoot{}, nil, err
	}
	return InspectFirstTaskSetup(db, runID, workspaceRoot)
}
