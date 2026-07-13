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

	"mc/verbs"
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: mc <verb> …")
		return 2
	}

	// Self-delegation: host-side mc with no direct spine path but a named
	// warm helper delegates the whole invocation into the lock domain.
	if os.Getenv("MC_HELPER") != "" && os.Getenv("MC_SPINE") == "" {
		return delegate(args, stdin, stdout, stderr)
	}

	out, err := dispatchVerb(args, stdin)
	if err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		var usage *verbs.UsageError
		if errors.As(err, &usage) {
			return 2
		}
		return 1
	}
	if err := writeJSON(stdout, out); err != nil {
		fmt.Fprintln(stderr, "mc:", err)
		return 2
	}
	return 0
}

func delegate(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	helper := os.Getenv("MC_HELPER")
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
		fmt.Fprintln(stderr, "mc: self-delegation failed:", err)
		return 2
	}
	return 0
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
	case "init":
		return cmdInit(rest)
	case "task":
		return cmdTask(rest)
	case "packet":
		return cmdPacket(rest)
	case "dispatch":
		return withSpine(func(db *sql.DB) (any, error) { return verbs.Dispatch(db) })
	case "complete":
		return cmdComplete(rest)
	case "editor":
		return cmdEditor(rest, stdin)
	case "strategist":
		return cmdStrategist(rest, stdin)
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
	db, err := verbs.OpenSpine(os.Getenv("MC_SPINE"))
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

func cmdInit(args []string) (any, error) {
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
		a.Spine = os.Getenv("MC_SPINE")
	}
	return verbs.Init(a)
}

func cmdTask(args []string) (any, error) {
	if len(args) == 0 {
		return nil, verbs.Usagef("usage: mc task add|get|block|unblock …")
	}
	switch args[0] {
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
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.TaskAdd(db, title, *worksource, *description, pri)
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
	}
	return nil, verbs.Usagef("unknown subverb: mc task %s", args[0])
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
		return withSpine(func(db *sql.DB) (any, error) {
			return verbs.PacketDecide(db, task, decision, *reason)
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
	if len(args) == 0 || args[0] != "decide" {
		return nil, verbs.Usagef("usage: mc editor decide --run <id> --batch -")
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

func cmdStrategist(args []string, stdin io.Reader) (any, error) {
	if len(args) == 0 || args[0] != "propose" {
		return nil, verbs.Usagef("usage: mc strategist propose --run <id> --batch -")
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
	return withSpine(func(db *sql.DB) (any, error) {
		return verbs.LandReport(db, task, *status, *reason)
	})
}
