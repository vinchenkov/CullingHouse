// Command mc is the Mission Control binary — the sole spine writer (§11.5).
// The Phase 1b skeleton verb surface is pinned by docs/phase1b-contract.md
// §2: single JSON object on stdout, diagnostics on stderr, exit 0 success /
// 1 domain rejection / 2 usage or environment error.
//
// Spine reachability (contract §1, skeleton env pair): MC_SPINE names the
// directly reachable spine file; when MC_HELPER=<container> is set and
// MC_SPINE is not, mc self-delegates — it re-invokes itself as
// `docker exec -i <helper> mc <argv…>`, passing stdin/stdout/stderr and the
// exit code through untouched (§11.5 "self-delegation, decided").
package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"mc/substrate"
	"mc/verbs"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: mc <verb> …")
		writeErrorEnvelope(stdout, stderr, "usage", "usage: mc <verb> …")
		return 2
	}
	if args[0] == "__dispatch-prepare" || args[0] == "__dispatch-commit" {
		return runPrivateDispatch(args, stdin, stdout, stderr)
	}
	// The launch-time identity recheck reads HOST files by definition: it must
	// run locally even when every ordinary verb self-delegates into the helper.
	if args[0] == "__mount-recheck" || args[0] == "__task-parent-recheck" || args[0] == "__task-skeleton-recover" || args[0] == "__setup-first-task" || args[0] == "__setup-accepted-seal" {
		return runLocal(args, stdin, stdout, stderr)
	}

	// Self-delegation: host-side mc with no direct spine path but a named
	// warm helper delegates the whole invocation into the lock domain.
	if shouldDelegateToHelper() {
		if args[0] == "dispatch" {
			return brokerDispatch(args, stdin, stdout, stderr)
		}
		return delegate(args, stdin, stdout, stderr)
	}

	return runLocal(args, stdin, stdout, stderr)
}

func runLocal(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	out, err := dispatchVerb(args, stdin)
	if err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		exitCode := 1
		errorCode := "domain-rejection"
		var usage *verbs.UsageError
		if errors.As(err, &usage) {
			exitCode = 2
			errorCode = "usage"
		} else {
			var domainErr *verbs.DomainError
			if errors.As(err, &domainErr) && domainErr.Code != "" {
				errorCode = domainErr.Code
			}
		}
		writeErrorEnvelope(stdout, stderr, errorCode, err.Error())
		return exitCode
	}
	if err := writeJSON(stdout, out); err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		return 2
	}
	return 0
}

func delegate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	helper := helperContainerName()
	argv := append([]string{"exec", "-i", helper, "mc"}, args...)
	cmd := exec.Command("docker", argv...)
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return exit.ExitCode()
		}
		// Wrapper-only failure: the inner mc never ran, so there are no
		// delegated bytes to preserve — emit the local envelope.
		fmt.Fprintln(stderr, "mc: self-delegation failed:", err)
		writeErrorEnvelope(stdout, stderr, "usage", "self-delegation failed: "+err.Error())
		return 2
	}
	return 0
}

func writeErrorEnvelope(stdout, stderr io.Writer, code, message string) {
	envelope := map[string]any{
		"error": map[string]any{"code": code, "message": message},
	}
	if err := writeJSON(stdout, envelope); err != nil {
		fmt.Fprintln(stderr, "mc: write error JSON:", err)
	}
}

func writeJSON(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(b))
	return err
}

// dispatchVerb parses the verb path and flags, opens the spine, and calls
// the verbs package. Every verb is a thin CLI layer over a testable
// function (contract §8).
func dispatchVerb(args []string, stdin io.Reader) (any, error) {
	verb := args[0]
	rest := args[1:]
	switch verb {
	case "__task-parent-recheck":
		if len(rest) != 1 {
			return nil, verbs.Usagef("usage: mc __task-parent-recheck <step-json>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(id, "mc __task-parent-recheck"); err != nil {
			return nil, err
		}
		var step verbs.PrivateDispatchTaskPrecreate
		dec := json.NewDecoder(strings.NewReader(rest[0]))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&step); err != nil {
			return nil, verbs.Usagef("task parent recheck step is invalid: %v", err)
		}
		if dec.More() {
			return nil, verbs.Usagef("task parent recheck step carries trailing data")
		}
		return verbs.TaskParentRecheck(step)
	case "__task-skeleton-recover":
		if len(rest) != 1 {
			return nil, verbs.Usagef("usage: mc __task-skeleton-recover <step-json>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(id, "mc __task-skeleton-recover"); err != nil {
			return nil, err
		}
		var step verbs.PrivateDispatchTaskPrecreate
		dec := json.NewDecoder(strings.NewReader(rest[0]))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&step); err != nil {
			return nil, verbs.Usagef("task skeleton recovery step is invalid: %v", err)
		}
		if dec.More() {
			return nil, verbs.Usagef("task skeleton recovery step carries trailing data")
		}
		return verbs.RecoverTaskSkeleton(step)
	case "__mount-recheck":
		if len(rest) != 1 {
			return nil, verbs.Usagef("usage: mc __mount-recheck <plan-file>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(id, "mc __mount-recheck"); err != nil {
			return nil, err
		}
		return verbs.MountRecheck(rest[0])
	case "__setup-first-task":
		if len(rest) != 1 {
			return nil, verbs.Usagef("usage: mc __setup-first-task <envelope-file>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(id, "mc __setup-first-task"); err != nil {
			return nil, err
		}
		env, err := verbs.ReadSetupEnvelope(rest[0])
		if err != nil {
			return nil, err
		}
		return verbs.RunFirstTaskSetup(env)
	case "__setup-accepted-seal":
		if len(rest) != 1 {
			return nil, verbs.Usagef("usage: mc __setup-accepted-seal <envelope-file>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(id, "mc __setup-accepted-seal"); err != nil {
			return nil, err
		}
		env, err := verbs.ReadSetupEnvelope(rest[0])
		if err != nil {
			return nil, err
		}
		return verbs.RunAcceptedSealSetup(env)
	case "onboard":
		return cmdOnboard(rest)
	case "init":
		return cmdInit(rest)
	case "task":
		return cmdTask(rest)
	case "initiative":
		return cmdInitiative(rest)
	case "worksource":
		return cmdWorksource(rest)
	case "packet":
		return cmdPacket(rest)
	case "dispatch":
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(id, "mc dispatch"); err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.DispatchWithProtocolIdentity(db, releaseBuildID, gatewayControlVersion,
				substrate.CurrentSchemaVersion, configSchemaVersion)
		})
	case "complete":
		return cmdComplete(rest)
	case "editor":
		return cmdEditor(rest, stdin)
	case "strategist":
		return cmdStrategist(rest, stdin)
	case "console":
		return cmdConsole(rest)
	case "homie":
		return cmdHomie(rest)
	case "outbox":
		return cmdOutbox(rest)
	case "verifier":
		return cmdVerifier(rest)
	case "heartbeat":
		if len(rest) != 1 {
			return nil, verbs.Usagef("usage: mc heartbeat <run_id>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) { return verbs.Heartbeat(db, id, rest[0]) })
	case "run":
		return cmdRun(rest)
	case "land":
		return cmdLand(rest)
	case "doctor":
		if len(rest) != 0 {
			return nil, verbs.Usagef("usage: mc doctor")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		// Doctor opens (or reports on) the spine itself: it must diagnose a
		// missing spine rather than fail on it, and must never create bytes.
		return verbs.Doctor(id, helperSpinePath())
	case "backup":
		if len(rest) != 0 {
			return nil, verbs.Usagef("usage: mc backup")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return verbs.Backup(id, helperSpinePath())
	case "reset":
		fs := newFlags("mc reset")
		confirm := fs.Bool("confirm", false, "confirm the destructive re-initialization")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return verbs.Reset(id, helperSpinePath(), *confirm)
	case "lock":
		// Pure read (§18 `mc <record> get`): the e2e's lease-state assertion
		// channel — it cannot open the spine volume (contract §7).
		if len(rest) != 1 || rest[0] != "get" {
			return nil, verbs.Usagef("usage: mc lock get")
		}
		return withSpine(func(db *sql.DB) (any, error) { return verbs.LockGet(db) })
	}
	return nil, verbs.Usagef("unknown verb %q", verb)
}

// withSpine opens MC_SPINE and runs fn.
func withSpine(fn func(db *sql.DB) (any, error)) (any, error) {
	db, err := verbs.OpenSpine(helperSpinePath())
	if err != nil {
		return nil, err
	}
	defer db.Close()
	return fn(db)
}

func newFlags(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parse(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		return verbs.Usagef("%s: %v", fs.Name(), err)
	}
	if fs.NArg() != 0 {
		return verbs.Usagef("%s: unexpected argument %q", fs.Name(), fs.Arg(0))
	}
	return nil
}

// positional peels the verb's one leading positional argument (Go's flag
// package stops at the first non-flag, so `mc complete <task> --run …`
// parses positional-first).
func positional(name string, args []string) (string, []string, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return "", nil, verbs.Usagef("%s: missing argument", name)
	}
	return args[0], args[1:], nil
}

func positionalID(name string, args []string) (int64, []string, error) {
	s, rest, err := positional(name, args)
	if err != nil {
		return 0, nil, err
	}
	id, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, nil, verbs.Usagef("%s: bad id %q", name, s)
	}
	return id, rest, nil
}

func cmdOnboard(args []string) (any, error) {
	a := verbs.OnboardArgs{Spine: helperSpinePath()}
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		a.Section = args[0]
		args = args[1:]
	}
	fs := newFlags("mc onboard")
	consoleHour, consoleMinute, consoleTZ := -1, -1, ""
	fs.BoolVar(&a.Smoke, "smoke", false, "run the full-pipeline smoke")
	fs.StringVar(&a.Worksource, "worksource", "", "first Worksource id")
	fs.StringVar(&a.WorkspaceRoot, "workspace-root", "", "first Worksource root")
	fs.IntVar(&a.TimeoutMinutes, "timeout-minutes", 0, "lease timeout")
	fs.IntVar(&a.GraceMinutes, "grace-minutes", 0, "lease grace")
	fs.IntVar(&a.HeartbeatIntervalS, "heartbeat-interval-s", 0, "heartbeat interval")
	fs.IntVar(&a.SpawnGraceS, "spawn-grace-s", 0, "first-heartbeat watchdog")
	fs.IntVar(&a.HardDeadlineMinutes, "hard-deadline-minutes", 0, "non-renewable hard deadline")
	fs.IntVar(&consoleHour, "console-hour", -1, "Daily Console delivery hour (0..23)")
	fs.IntVar(&consoleMinute, "console-minute", -1, "Daily Console delivery minute (0..59)")
	fs.StringVar(&consoleTZ, "console-tz", "", "Daily Console IANA timezone")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	for _, item := range []struct {
		name  string
		value int
	}{
		{"timeout-minutes", a.TimeoutMinutes},
		{"grace-minutes", a.GraceMinutes},
		{"heartbeat-interval-s", a.HeartbeatIntervalS},
		{"spawn-grace-s", a.SpawnGraceS},
		{"hard-deadline-minutes", a.HardDeadlineMinutes},
	} {
		if item.value < 0 {
			return nil, verbs.Usagef("mc onboard --%s must be non-negative", item.name)
		}
	}
	provided := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { provided[f.Name] = true })
	consoleSet := provided["console-hour"] || provided["console-minute"] || provided["console-tz"]
	if consoleSet {
		if !provided["console-hour"] || !provided["console-minute"] || !provided["console-tz"] {
			return nil, verbs.Usagef("mc onboard console schedule requires --console-hour, --console-minute, and --console-tz together")
		}
		a.ConsoleScheduleSet = true
		a.ConsoleHour = consoleHour
		a.ConsoleMinute = consoleMinute
		a.ConsoleTZ = consoleTZ
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return verbs.Onboard(id, a)
}

func cmdInit(args []string) (any, error) {
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	if err := verbs.RequireHostScope(id, "mc init"); err != nil {
		return nil, err
	}
	fs := newFlags("mc init")
	a := verbs.InitArgs{}
	consoleHour, consoleMinute, consoleTZ := -1, -1, ""
	fs.StringVar(&a.Spine, "spine", "", "spine path to provision")
	fs.StringVar(&a.Worksource, "worksource", "", "worksource id")
	fs.StringVar(&a.WorkspaceRoot, "workspace-root", "", "workspace root path")
	fs.IntVar(&a.TimeoutMinutes, "timeout-minutes", 0, "lease timeout")
	fs.IntVar(&a.GraceMinutes, "grace-minutes", 0, "lease grace")
	fs.IntVar(&a.HeartbeatIntervalS, "heartbeat-interval-s", 0, "heartbeat interval")
	fs.IntVar(&a.SpawnGraceS, "spawn-grace-s", 0, "first-heartbeat watchdog")
	fs.IntVar(&a.HardDeadlineMinutes, "hard-deadline-minutes", 0, "non-renewable hard deadline")
	fs.IntVar(&consoleHour, "console-hour", -1, "Daily Console delivery hour (0..23)")
	fs.IntVar(&consoleMinute, "console-minute", -1, "Daily Console delivery minute (0..59)")
	fs.StringVar(&consoleTZ, "console-tz", "", "Daily Console IANA timezone")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	consoleSet := consoleHour != -1 || consoleMinute != -1 || consoleTZ != ""
	if consoleSet {
		if consoleHour == -1 || consoleMinute == -1 || consoleTZ == "" {
			return nil, verbs.Usagef("mc init console schedule requires --console-hour, --console-minute, and --console-tz together")
		}
		a.ConsoleScheduleSet = true
		a.ConsoleHour = consoleHour
		a.ConsoleMinute = consoleMinute
		a.ConsoleTZ = consoleTZ
	}
	if a.Spine == "" {
		a.Spine = helperSpinePath()
	}
	return verbs.Init(a)
}

func cmdTask(args []string) (any, error) {
	if len(args) == 0 {
		return nil, verbs.Usagef("usage: mc task add|get|block|unblock|setup-register|setup-continue …")
	}
	switch args[0] {
	case "setup-register":
		fs := newFlags("mc task setup-register")
		runID := fs.String("run", "", "pipeline run id")
		taskID := fs.Int64("task", 0, "task id")
		device := fs.String("device", "", "registered task-root device")
		inode := fs.String("inode", "", "registered task-root inode")
		ownerUID := fs.Int("owner-uid", -1, "registered task-root owner uid")
		if err := parse(fs, args[1:]); err != nil {
			return nil, err
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(idn, "mc task setup-register"); err != nil {
			return nil, err
		}
		receipt := verbs.TaskSetupReceipt{RunID: *runID, TaskID: *taskID,
			Root: verbs.TaskSetupIdentity{Device: *device, Inode: *inode, OwnerUID: *ownerUID}}
		return withSpine(func(db *sql.DB) (any, error) { return verbs.RegisterFirstTaskSetup(db, receipt) })
	case "setup-record":
		fs := newFlags("mc task setup-record")
		runID := fs.String("run", "", "pipeline run id")
		workspace := fs.String("workspace", "", "worksource workspace root")
		result := fs.String("result", "", "setup result JSON from the setup container")
		if err := parse(fs, args[1:]); err != nil {
			return nil, err
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(idn, "mc task setup-record"); err != nil {
			return nil, err
		}
		dec := json.NewDecoder(strings.NewReader(*result))
		dec.DisallowUnknownFields()
		var res verbs.SetupResult
		if err := dec.Decode(&res); err != nil {
			return nil, verbs.Usagef("setup result is invalid: %v", err)
		}
		return withSpine(func(db *sql.DB) (any, error) {
			root, rows, err := verbs.RecordFirstTaskSetupClosure(db, *runID, *workspace, res)
			if err != nil {
				return nil, err
			}
			return map[string]any{"task_id": root.Receipt.TaskID, "rows": len(rows)}, nil
		})
	case "setup-continue":
		fs := newFlags("mc task setup-continue")
		runID := fs.String("run", "", "pipeline run id")
		if err := parse(fs, args[1:]); err != nil {
			return nil, err
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		if err := verbs.RequireHostScope(idn, "mc task setup-continue"); err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.ContinueFirstTaskSetup(db, *runID)
		})
	case "add":
		title, rest, err := positional("mc task add", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc task add")
		worksource := fs.String("worksource", "", "worksource id")
		description := fs.String("description", "", "task description")
		priority := fs.Int("priority", -100, "priority (-1..3)")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		var pri *int
		if *priority != -100 {
			pri = priority
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.TaskAdd(db, idn, title, *worksource, *description, pri)
		})
	case "get":
		if len(args) != 2 {
			return nil, verbs.Usagef("usage: mc task get <id>")
		}
		id, err := strconv.ParseInt(args[1], 10, 64)
		if err != nil {
			return nil, verbs.Usagef("mc task get: bad id %q", args[1])
		}
		return withSpine(func(db *sql.DB) (any, error) { return verbs.TaskGet(db, id) })
	case "block":
		id, rest, err := positionalID("mc task block", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc task block")
		reason := fs.String("reason", "", "operator decision required")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.TaskBlock(db, idn, id, *reason)
		})
	case "unblock":
		id, rest, err := positionalID("mc task unblock", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc task unblock")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.TaskUnblock(db, idn, id)
		})
	case "interrupt":
		id, rest, err := positionalID("mc task interrupt", args[1:])
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, verbs.Usagef("mc task interrupt: unexpected argument %q", rest[0])
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.TaskInterrupt(db, idn, id)
		})
	}
	return nil, verbs.Usagef("unknown subverb: mc task %s", args[0])
}

func cmdInitiative(args []string) (any, error) {
	if len(args) == 0 || args[0] != "add" {
		return nil, verbs.Usagef("usage: mc initiative add <title> --worksource <id> --charter <criteria> [--priority -1..3]")
	}
	title, rest, err := positional("mc initiative add", args[1:])
	if err != nil {
		return nil, err
	}
	fs := newFlags("mc initiative add")
	worksource := fs.String("worksource", "", "worksource id")
	charter := fs.String("charter", "", "checkable initiative charter")
	priority := fs.Int("priority", -100, "priority (-1..3)")
	if err := parse(fs, rest); err != nil {
		return nil, err
	}
	var pri *int
	if *priority != -100 {
		pri = priority
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) {
		return verbs.InitiativeAdd(db, id, title, *worksource, *charter, pri)
	})
}

func cmdWorksource(args []string) (any, error) {
	if len(args) == 0 {
		return nil, verbs.Usagef("usage: mc worksource add|list|pause|archive …")
	}
	switch args[0] {
	case "list":
		if len(args) != 1 {
			return nil, verbs.Usagef("usage: mc worksource list")
		}
		return withSpine(func(db *sql.DB) (any, error) { return verbs.WorksourceList(db) })
	case "add":
		worksourceID, rest, err := positional("mc worksource add", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc worksource add")
		a := verbs.WorksourceAddArgs{ID: worksourceID}
		fs.StringVar(&a.Title, "title", "", "display title")
		fs.StringVar(&a.Kind, "kind", "", "repo|personal|transient")
		fs.StringVar(&a.Jurisdiction, "jurisdiction", "", "jurisdiction description")
		fs.StringVar(&a.SandboxProfile, "sandbox-profile", "", "sandbox profile id")
		fs.StringVar(&a.Directive, "directive", "", "standing directive")
		fs.StringVar(&a.SeedingMode, "seeding-mode", "propose-only", "propose-only|auto")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) { return verbs.WorksourceAdd(db, id, a) })
	case "pause", "archive":
		worksourceID, rest, err := positional("mc worksource "+args[0], args[1:])
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, verbs.Usagef("mc worksource %s: unexpected argument %q", args[0], rest[0])
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		target := "paused"
		if args[0] == "archive" {
			target = "archived"
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.WorksourceSetStatus(db, id, worksourceID, target)
		})
	}
	return nil, verbs.Usagef("unknown subverb: mc worksource %s", args[0])
}

func cmdPacket(args []string) (any, error) {
	if len(args) == 0 {
		return nil, verbs.Usagef("usage: mc packet decide|list …")
	}
	switch args[0] {
	case "decide":
		task, rest, err := positionalID("mc packet decide", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc packet decide")
		approve := fs.Bool("approve", false, "approve the packet")
		revise := fs.Bool("revise", false, "revise (Phase 2)")
		cancel := fs.Bool("cancel", false, "cancel (Phase 2)")
		reason := fs.String("reason", "", "decision reason")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		decision := ""
		n := 0
		for name, set := range map[string]bool{"approve": *approve, "revise": *revise, "cancel": *cancel} {
			if set {
				decision = name
				n++
			}
		}
		if n != 1 {
			return nil, verbs.Usagef("mc packet decide requires exactly one of --approve, --revise, --cancel")
		}
		idn, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.PacketDecide(db, idn, task, decision, *reason)
		})
	case "list":
		return withSpine(func(db *sql.DB) (any, error) { return verbs.PacketList(db) })
	}
	return nil, verbs.Usagef("unknown subverb: mc packet %s", args[0])
}

func cmdComplete(args []string) (any, error) {
	task, rest, err := positionalID("mc complete", args)
	if err != nil {
		return nil, err
	}
	fs := newFlags("mc complete")
	a := verbs.CompleteArgs{Task: task}
	fs.StringVar(&a.Run, "run", "", "fencing token")
	fs.StringVar(&a.Status, "status", "", "worked|packaged")
	fs.StringVar(&a.Branch, "branch", "", "branch the work landed on")
	fs.StringVar(&a.Outputs, "outputs", "", "output artifact path")
	reason := fs.String("reason", "", "terminal reason")
	needsOperator := fs.Bool("needs-operator", false, "block for an operator decision")
	infra := fs.Bool("infra", false, "charge the dispatch-infrastructure budget")
	correctionCount := fs.Int("correction-count", -1, "reserved; verifier verdict owns correction arithmetic")
	if err := parse(fs, rest); err != nil {
		return nil, err
	}
	if *correctionCount != -1 {
		return nil, verbs.Domainf("--correction-count is written only by mc verifier verdict; mc complete cannot own the quality budget (§7/§10)")
	}
	a.Reason = *reason
	a.NeedsOperator = *needsOperator
	a.Infra = *infra
	if a.Run == "" {
		return nil, verbs.Usagef("mc complete requires --run (the fencing token, §10)")
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) { return verbs.Complete(db, id, a) })
}

// batchReader validates --batch - and returns the payload stream (ADR-001
// D1: batch payloads arrive as JSON on stdin).
func batchReader(batch string, stdin io.Reader) (io.Reader, error) {
	if batch != "-" {
		return nil, verbs.Usagef("--batch supports only '-' (JSON on stdin, ADR-001 D1)")
	}
	return stdin, nil
}

func cmdEditor(args []string, stdin io.Reader) (any, error) {
	if len(args) == 0 || (args[0] != "decide" && args[0] != "plan-review") {
		return nil, verbs.Usagef("usage: mc editor decide --run <id> --batch - | " +
			"mc editor plan-review --run <id> --initiative <id> --verdict pass|send-back [--reason <prose>]")
	}
	if args[0] == "plan-review" {
		return cmdEditorPlanReview(args[1:])
	}
	fs := newFlags("mc editor decide")
	run := fs.String("run", "", "fencing token")
	batch := fs.String("batch", "", "batch payload source")
	if err := parse(fs, args[1:]); err != nil {
		return nil, err
	}
	if *run == "" {
		return nil, verbs.Usagef("mc editor decide requires --run")
	}
	r, err := batchReader(*batch, stdin)
	if err != nil {
		return nil, err
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) { return verbs.EditorDecide(db, id, *run, r) })
}

// cmdEditorPlanReview wires the Editor's holistic wave terminal (ADR-020 D5).
func cmdEditorPlanReview(args []string) (any, error) {
	fs := newFlags("mc editor plan-review")
	run := fs.String("run", "", "fencing token")
	initiative := fs.Int64("initiative", 0, "the initiative under review")
	verdict := fs.String("verdict", "", "pass | send-back")
	reason := fs.String("reason", "", "the objection (required for send-back, forbidden for pass)")
	if err := parse(fs, args); err != nil {
		return nil, err
	}
	if *run == "" {
		return nil, verbs.Usagef("mc editor plan-review requires --run")
	}
	if *initiative == 0 {
		return nil, verbs.Usagef("mc editor plan-review requires --initiative")
	}
	if *verdict == "" {
		return nil, verbs.Usagef("mc editor plan-review requires --verdict pass|send-back")
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) {
		return verbs.EditorPlanReview(db, id, verbs.PlanReviewArgs{
			Run: *run, Initiative: *initiative, Verdict: *verdict, Reason: *reason,
		})
	})
}

func cmdStrategist(args []string, stdin io.Reader) (any, error) {
	if len(args) == 0 || (args[0] != "propose" && args[0] != "wave") {
		return nil, verbs.Usagef("usage: mc strategist propose --run <id> --batch - | " +
			"mc strategist wave --run <id> --initiative <id> --batch -")
	}
	// The wave terminal: unblocked by ADR-020, which gives the Editor's
	// mandatory holistic plan review a durable state and a dispatch slot.
	if args[0] == "wave" {
		fs := newFlags("mc strategist wave")
		run := fs.String("run", "", "fencing token")
		initiative := fs.Int64("initiative", 0, "the initiative the wave is born into")
		batch := fs.String("batch", "", "batch payload source")
		if err := parse(fs, args[1:]); err != nil {
			return nil, err
		}
		if *run == "" {
			return nil, verbs.Usagef("mc strategist wave requires --run")
		}
		if *initiative == 0 {
			return nil, verbs.Usagef("mc strategist wave requires --initiative")
		}
		r, err := batchReader(*batch, stdin)
		if err != nil {
			return nil, err
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.StrategistWave(db, id, *run, *initiative, r)
		})
	}
	fs := newFlags("mc strategist propose")
	run := fs.String("run", "", "fencing token")
	batch := fs.String("batch", "", "batch payload source")
	if err := parse(fs, args[1:]); err != nil {
		return nil, err
	}
	if *run == "" {
		return nil, verbs.Usagef("mc strategist propose requires --run")
	}
	r, err := batchReader(*batch, stdin)
	if err != nil {
		return nil, err
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) { return verbs.StrategistPropose(db, id, *run, r) })
}

func cmdConsole(args []string) (any, error) {
	if len(args) == 0 || args[0] != "publish" {
		return nil, verbs.Usagef("usage: mc console publish --run <id> --content <path>")
	}
	fs := newFlags("mc console publish")
	run := fs.String("run", "", "fencing token")
	content := fs.String("content", "", "rendered Console path beneath outputs/")
	if err := parse(fs, args[1:]); err != nil {
		return nil, err
	}
	if *run == "" {
		return nil, verbs.Usagef("mc console publish requires --run")
	}
	if *content == "" {
		return nil, verbs.Usagef("mc console publish requires --content")
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) {
		return verbs.ConsolePublish(db, id, *run, *content)
	})
}

func cmdHomie(args []string) (any, error) {
	if len(args) == 0 {
		return nil, verbs.Usagef("usage: mc homie start|bind|send|claim|reply|list|history|resume|end …")
	}
	switch args[0] {
	case "start":
		fs := newFlags("mc homie start")
		from := fs.String("from", "", "initial surface:channel_ref")
		allowSet := false
		allowRaw := ""
		fs.Func("allow", "comma-separated Homie-agent verbs, or none", func(value string) error {
			if allowSet {
				return fmt.Errorf("--allow may be supplied only once")
			}
			allowSet = true
			allowRaw = value
			return nil
		})
		if err := parse(fs, args[1:]); err != nil {
			return nil, err
		}
		if *from == "" {
			return nil, verbs.Usagef("mc homie start requires --from <surface:channel_ref>")
		}
		var allowlist *[]string
		if allowSet {
			values := []string{}
			if allowRaw != "none" {
				values = strings.Split(allowRaw, ",")
			}
			allowlist = &values
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieStart(db, id, verbs.HomieStartArgs{From: *from, Allowlist: allowlist})
		})

	case "bind":
		sessionID, rest, err := positional("mc homie bind", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc homie bind")
		from := fs.String("from", "", "surface:channel_ref")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		if *from == "" {
			return nil, verbs.Usagef("mc homie bind requires --from <surface:channel_ref>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieBind(db, id, sessionID, *from)
		})

	case "resume":
		sessionID, rest, err := positional("mc homie resume", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc homie resume")
		from := fs.String("from", "", "surface:channel_ref")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		if *from == "" {
			return nil, verbs.Usagef("mc homie resume requires --from <surface:channel_ref>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieResume(db, id, sessionID, *from)
		})

	case "list":
		if len(args) != 1 {
			return nil, verbs.Usagef("usage: mc homie list")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieList(db, id)
		})

	case "send":
		sessionID, rest, err := positional("mc homie send", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc homie send")
		from := fs.String("from", "", "origin surface:channel_ref")
		body := fs.String("body", "", "inbound text")
		attachments := fs.String("attachments", "", "JSON array of file-plane paths")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		if *from == "" {
			return nil, verbs.Usagef("mc homie send requires --from <surface:channel_ref>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieSend(db, id, verbs.HomieSendArgs{
				Session: sessionID, From: *from, Body: *body, Attachments: *attachments,
			})
		})

	case "claim":
		sessionID, rest, err := positional("mc homie claim", args[1:])
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, verbs.Usagef("mc homie claim: unexpected argument %q", rest[0])
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieClaim(db, id, sessionID)
		})

	case "reply":
		sessionID, rest, err := positional("mc homie reply", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc homie reply")
		to := fs.Int64("to", 0, "claimed inbound message id")
		body := fs.String("body", "", "reply text")
		attachments := fs.String("attachments", "", "JSON array of file-plane paths")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		if *to <= 0 {
			return nil, verbs.Usagef("mc homie reply requires --to <inbound message id>")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieReply(db, id, verbs.HomieReplyArgs{
				Session: sessionID, To: *to, Body: *body, Attachments: *attachments,
			})
		})

	case "history":
		sessionID, rest, err := positional("mc homie history", args[1:])
		if err != nil {
			return nil, err
		}
		if len(rest) != 0 {
			return nil, verbs.Usagef("mc homie history: unexpected argument %q", rest[0])
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieHistory(db, id, sessionID)
		})

	case "end":
		sessionID, rest, err := positional("mc homie end", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc homie end")
		reason := fs.String("reason", "", "end reason")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		if *reason == "" {
			return nil, verbs.Usagef("mc homie end requires --reason")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.HomieEnd(db, id, sessionID, *reason)
		})
	}
	return nil, verbs.Usagef("unknown subverb: mc homie %s", args[0])
}

func cmdOutbox(args []string) (any, error) {
	if len(args) == 0 {
		return nil, verbs.Usagef("usage: mc outbox poll|ack …")
	}
	switch args[0] {
	case "poll":
		fs := newFlags("mc outbox poll")
		surface := fs.String("surface", "", "delivery surface")
		limit := fs.Int("limit", 100, "maximum rows")
		if err := parse(fs, args[1:]); err != nil {
			return nil, err
		}
		if *surface == "" {
			return nil, verbs.Usagef("mc outbox poll requires --surface")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		// Refusal precedes spine open (wave-2 contract §1).
		if err := verbs.RequireHostScope(id, "mc outbox poll"); err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.OutboxPoll(db, id, *surface, *limit)
		})

	case "ack":
		rowID, rest, err := positionalID("mc outbox ack", args[1:])
		if err != nil {
			return nil, err
		}
		fs := newFlags("mc outbox ack")
		surface := fs.String("surface", "", "delivery surface")
		if err := parse(fs, rest); err != nil {
			return nil, err
		}
		if *surface == "" {
			return nil, verbs.Usagef("mc outbox ack requires --surface")
		}
		id, err := verbs.LoadIdentity()
		if err != nil {
			return nil, err
		}
		// Refusal precedes spine open (wave-2 contract §1).
		if err := verbs.RequireHostScope(id, "mc outbox ack"); err != nil {
			return nil, err
		}
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.OutboxAck(db, id, rowID, *surface)
		})
	}
	return nil, verbs.Usagef("unknown subverb: mc outbox %s", args[0])
}

func cmdVerifier(args []string) (any, error) {
	if len(args) == 0 || args[0] != "verdict" {
		return nil, verbs.Usagef("usage: mc verifier verdict <task> --run <id> --outcome … --evidence … --sha …")
	}
	task, rest, err := positionalID("mc verifier verdict", args[1:])
	if err != nil {
		return nil, err
	}
	fs := newFlags("mc verifier verdict")
	run := fs.String("run", "", "fencing token")
	outcome := fs.String("outcome", "", "pass|correct|budget-spent")
	evidence := fs.String("evidence", "", "gate-ladder evidence path")
	sha := fs.String("sha", "", "the verified commit SHA")
	correction := fs.String("correction", "", "correction file path (CORRECT)")
	deepening := fs.String("deepening", "", "genuine|churn for a refinement verdict")
	if err := parse(fs, rest); err != nil {
		return nil, err
	}
	if *run == "" {
		return nil, verbs.Usagef("mc verifier verdict requires --run")
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) {
		return verbs.VerifierVerdict(db, id, verbs.VerdictArgs{
			Task: task, Run: *run, Outcome: *outcome, Evidence: *evidence,
			SHA: *sha, Correction: *correction, Deepening: *deepening,
		})
	})
}

func cmdRun(args []string) (any, error) {
	if len(args) == 1 && args[0] == "list" {
		// Pure read (§18 `mc <record> list`): the e2e's runs-history channel.
		return withSpine(func(db *sql.DB) (any, error) { return verbs.RunList(db) })
	}
	if len(args) == 0 || args[0] != "register-session" {
		return nil, verbs.Usagef("usage: mc run register-session <run_id> --native-ref <ref> --file <name> | mc run list")
	}
	runID, rest, err := positional("mc run register-session", args[1:])
	if err != nil {
		return nil, err
	}
	fs := newFlags("mc run register-session")
	nativeRef := fs.String("native-ref", "", "harness native session handle")
	file := fs.String("file", "", "trace filename")
	if err := parse(fs, rest); err != nil {
		return nil, err
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) {
		return verbs.RegisterSession(db, id, runID, *nativeRef, *file)
	})
}

func cmdLand(args []string) (any, error) {
	if len(args) == 0 || args[0] != "report" {
		return nil, verbs.Usagef("usage: mc land report <task> --status success|failure [--reason …]")
	}
	task, rest, err := positionalID("mc land report", args[1:])
	if err != nil {
		return nil, err
	}
	fs := newFlags("mc land report")
	status := fs.String("status", "", "success|failure")
	reason := fs.String("reason", "", "landing failure reason")
	if err := parse(fs, rest); err != nil {
		return nil, err
	}
	id, err := verbs.LoadIdentity()
	if err != nil {
		return nil, err
	}
	// Refusal precedes spine open (wave-2 contract §1): a pipeline caller
	// must not create spine bytes at a fresh MC_SPINE path.
	if err := verbs.RequireHostScope(id, "mc land report"); err != nil {
		return nil, err
	}
	return withSpine(func(db *sql.DB) (any, error) {
		return verbs.LandReport(db, id, task, *status, *reason)
	})
}
