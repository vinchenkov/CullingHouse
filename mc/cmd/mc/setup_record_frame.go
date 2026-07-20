package main

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"

	"mc/verbs"
)

// The setup-record crossing is two frames, not one verb.
//
// `mc task setup-record` and `mc task accepted-seal-record` both read HOST
// files (the Worksource task root and the store the setup container landed in
// it) AND write the spine. On the primary Darwin target the spine is reachable
// only by self-delegating into the helper container, which carries spine and
// MC_HOME binds and no Worksource — so the delegated whole-verb form resolved
// a host path from inside the helper and refused on a loop with
// `mc: source "<ws>" does not exist`.
//
// Neither of the two escapes works on its own: `runLocal` cannot reach the
// spine (host mc deliberately has no spine path, §11.5), and binding the
// Worksource into the helper would attest namespace-local device/inode values
// on Docker Desktop — the exact crossing defect 690fb08 fixed for the
// accepted-seal recheck.
//
// So the verb splits along the boundary the rest of this plane already uses:
// the host frame attests the filesystem and hands the delegated spine frame
// device/inode/owner identity, never a path. The `--task` id the host derives
// its path from is an input, not an authority — the spine frame refuses unless
// it matches its live lease AND the identity reproduces the durable receipt.

// recordFrameVerbs maps a resident-facing record verb to its path-free spine
// half. Membership here is what routes a verb through the host attest.
var recordFrameVerbs = map[string]string{
	"setup-record":         "setup-record-attested",
	"accepted-seal-record": "accepted-seal-record-attested",
}

// runSetupRecordFrame is the host half. It attests locally whether or not this
// build delegates, so the two lanes cannot diverge, then hands the identity-only
// argv to whichever frame owns the spine.
func runSetupRecordFrame(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	spineArgs, err := hostAttestRecord(args)
	if err != nil {
		return writeVerbError(stdout, stderr, err)
	}
	if shouldDelegateToHelper() {
		return delegate(spineArgs, stdin, stdout, stderr)
	}
	return runLocal(spineArgs, stdin, stdout, stderr)
}

func hostAttestRecord(args []string) ([]string, error) {
	verb := args[1]
	attested, ok := recordFrameVerbs[verb]
	if !ok {
		return nil, verbs.Usagef("mc task %s: not a record frame", verb)
	}
	fs := newFlags("mc task " + verb)
	runID := fs.String("run", "", "pipeline run id")
	taskID := fs.Int64("task", 0, "subject task id")
	workspace := fs.String("workspace", "", "worksource workspace root")
	result := fs.String("result", "", "setup result JSON from the setup container")
	if err := parse(fs, args[2:]); err != nil {
		return nil, err
	}
	idn, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	if err := verbs.RequireHostScope(idn, "mc task "+verb); err != nil {
		return nil, err
	}
	dec := json.NewDecoder(strings.NewReader(*result))
	dec.DisallowUnknownFields()
	var res verbs.SetupResult
	if err := dec.Decode(&res); err != nil {
		return nil, verbs.Usagef("setup result is invalid: %v", err)
	}
	if *taskID < 1 {
		return nil, verbs.Usagef("mc task %s: --task is required", verb)
	}

	var root verbs.FirstTaskSetupRoot
	switch verb {
	case "setup-record":
		root, _, err = verbs.HostAttestFirstTaskSetupClosure(*taskID, *workspace, res)
	case "accepted-seal-record":
		root, err = verbs.HostAttestAcceptedSealRebuild(*taskID, *workspace, res)
	}
	if err != nil {
		return nil, err
	}
	// `--result` crosses byte-exactly: the resident never reinterprets the
	// executor's evidence and neither does this frame.
	return []string{"task", attested,
		"--run", *runID,
		"--task", strconv.FormatInt(*taskID, 10),
		"--device", root.Receipt.Root.Device,
		"--inode", root.Receipt.Root.Inode,
		"--owner-uid", strconv.Itoa(root.Receipt.Root.OwnerUID),
		"--result", *result,
	}, nil
}

// attestedRecordFlags parses the path-free grammar of a spine half. It admits
// no workspace and no other host path by construction: whatever ran this frame
// may have no view of the Worksource at all.
func attestedRecordFlags(name string, args []string) (string, int64, verbs.TaskSetupIdentity, verbs.SetupResult, error) {
	fs := newFlags(name)
	runID := fs.String("run", "", "pipeline run id")
	taskID := fs.Int64("task", 0, "host-attested subject task id")
	device := fs.String("device", "", "host-attested task-root device")
	inode := fs.String("inode", "", "host-attested task-root inode")
	ownerUID := fs.Int("owner-uid", -1, "host-attested task-root owner uid")
	result := fs.String("result", "", "setup result JSON from the setup container")
	if err := parse(fs, args); err != nil {
		return "", 0, verbs.TaskSetupIdentity{}, verbs.SetupResult{}, err
	}
	idn, err := verbs.LoadIdentity()
	if err != nil {
		return "", 0, verbs.TaskSetupIdentity{}, verbs.SetupResult{}, err
	}
	if err := verbs.RequireHostScope(idn, name); err != nil {
		return "", 0, verbs.TaskSetupIdentity{}, verbs.SetupResult{}, err
	}
	dec := json.NewDecoder(strings.NewReader(*result))
	dec.DisallowUnknownFields()
	var res verbs.SetupResult
	if err := dec.Decode(&res); err != nil {
		return "", 0, verbs.TaskSetupIdentity{}, verbs.SetupResult{}, verbs.Usagef("setup result is invalid: %v", err)
	}
	if *taskID < 1 || *ownerUID < 0 {
		return "", 0, verbs.TaskSetupIdentity{}, verbs.SetupResult{}, verbs.Usagef("%s: --task and --owner-uid are required", name)
	}
	return *runID, *taskID, verbs.TaskSetupIdentity{Device: *device, Inode: *inode, OwnerUID: *ownerUID}, res, nil
}
