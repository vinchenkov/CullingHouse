// S5 spike probe — SQLite WAL + crash discipline on a named volume.
// Throwaway code; assertions get folded into the permanent suite later.
//
// Subcommands (all run INSIDE the linux/arm64 container):
//
//	guard       --dir D                          fail-closed mount check only
//	seed        --db F --rows N                  create schema + commit N baseline rows
//	write       --db F --rows N --tag T          BEGIN IMMEDIATE + commit N rows
//	pragmacheck --db F --conns N                 assert per-connection PRAGMAs on N pool conns
//	hang        --db F --rows N --tag T --pidfile P
//	                                             BEGIN IMMEDIATE, insert (spilling to WAL),
//	                                             write pidfile, sleep forever (kill -9 target)
//	read        --db F --expect N --max-ms M     concurrent reader: consistent count, not blocked
//	contend     --db F --busy-ms B --min-ms M    second writer: busy_timeout waits then SQLITE_BUSY
//	verify      --db F --expect N --absent-tag T reopen after crash: integrity_check, counts
//
// Every subcommand that touches the DB calls guard() first: opening the DB on
// anything but a block-device-backed local filesystem (i.e. a named volume
// inside the runtime VM) is REFUSED — spec Inv. 24, fail-closed.
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	busyDefaultMs = 5000
	blobBytes     = 64 * 1024
)

func main() {
	if len(os.Args) < 2 {
		die("usage: s5probe <guard|seed|write|pragmacheck|hang|read|contend|verify> [flags]")
	}
	cmd, args := os.Args[1], os.Args[2:]
	var err error
	switch cmd {
	case "guard":
		err = cmdGuard(args)
	case "seed":
		err = cmdSeed(args)
	case "write":
		err = cmdWrite(args)
	case "pragmacheck":
		err = cmdPragmaCheck(args)
	case "hang":
		err = cmdHang(args)
	case "read":
		err = cmdRead(args)
	case "contend":
		err = cmdContend(args)
	case "verify":
		err = cmdVerify(args)
	default:
		err = fmt.Errorf("unknown subcommand %q", cmd)
	}
	if err != nil {
		die("%s: FAIL: %v", cmd, err)
	}
	fmt.Printf("%s: OK\n", cmd)
}

func die(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}

// ---------- fail-closed mount guard (Inv. 24) ----------

type mountEntry struct {
	mountPoint string
	fstype     string
	source     string
}

func parseMountInfo(path string) ([]mountEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var out []mountEntry
	for _, line := range strings.Split(string(data), "\n") {
		if line == "" {
			continue
		}
		// format: ... mountPoint ... - fstype source superopts
		sep := strings.Index(line, " - ")
		if sep < 0 {
			continue
		}
		pre := strings.Fields(line[:sep])
		post := strings.Fields(line[sep+3:])
		if len(pre) < 5 || len(post) < 2 {
			continue
		}
		out = append(out, mountEntry{mountPoint: unescapeOctal(pre[4]), fstype: post[0], source: post[1]})
	}
	return out, nil
}

func unescapeOctal(s string) string {
	// mountinfo escapes space/tab/newline/backslash as \040 etc.
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) {
			var v int
			if _, err := fmt.Sscanf(s[i+1:i+4], "%o", &v); err == nil {
				b.WriteByte(byte(v))
				i += 3
				continue
			}
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// allowedFstypes is an ALLOWLIST: only local block-device-backed filesystems
// pass. virtiofs / fuse.* / grpcfuse / fakeowner / 9p / nfs / cifs / overlay
// all fail — fail-closed means anything unrecognized is refused too.
var allowedFstypes = map[string]bool{"ext4": true, "ext3": true, "xfs": true, "btrfs": true}

func guardDir(dir string) error {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return fmt.Errorf("cannot resolve %s: %w", abs, err)
	}
	entries, err := parseMountInfo("/proc/self/mountinfo")
	if err != nil {
		return fmt.Errorf("cannot read mountinfo (not linux?): %w — REFUSING (fail-closed)", err)
	}
	var best *mountEntry
	for i := range entries {
		mp := entries[i].mountPoint
		if resolved == mp || strings.HasPrefix(resolved, strings.TrimSuffix(mp, "/")+"/") {
			if best == nil || len(mp) > len(best.mountPoint) {
				best = &entries[i]
			}
		}
	}
	if best == nil {
		return fmt.Errorf("no mount found containing %s — REFUSING (fail-closed)", resolved)
	}
	if !allowedFstypes[best.fstype] || !strings.HasPrefix(best.source, "/dev/") {
		return fmt.Errorf("REFUSING to open DB under %s: mount=%s fstype=%s source=%s is not a block-device-backed local filesystem (named volume required, Inv. 24)",
			resolved, best.mountPoint, best.fstype, best.source)
	}
	fmt.Printf("guard: accepted %s (mount=%s fstype=%s source=%s)\n", resolved, best.mountPoint, best.fstype, best.source)
	return nil
}

func guardDB(dbPath string) error { return guardDir(filepath.Dir(dbPath)) }

func cmdGuard(args []string) error {
	fs := flag.NewFlagSet("guard", flag.ExitOnError)
	dir := fs.String("dir", "", "directory to check")
	fs.Parse(args)
	if *dir == "" {
		return fmt.Errorf("--dir required")
	}
	return guardDir(*dir)
}

// ---------- DB helpers ----------

func dsn(dbPath string, busyMs int, extra ...string) string {
	p := []string{
		fmt.Sprintf("_pragma=busy_timeout(%d)", busyMs),
		"_pragma=journal_mode(WAL)",
		"_pragma=synchronous(NORMAL)",
		"_pragma=foreign_keys(1)",
	}
	p = append(p, extra...)
	return "file:" + dbPath + "?" + strings.Join(p, "&")
}

func open(dbPath string, busyMs int, extra ...string) (*sql.DB, error) {
	if err := guardDB(dbPath); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", dsn(dbPath, busyMs, extra...))
	if err != nil {
		return nil, err
	}
	return db, nil
}

func pragmaStr(q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, name string) (string, error) {
	var v string
	err := q.QueryRowContext(context.Background(), "PRAGMA "+name).Scan(&v)
	return v, err
}

func assertWAL(db *sql.DB) error {
	jm, err := pragmaStr(db, "journal_mode")
	if err != nil {
		return err
	}
	if !strings.EqualFold(jm, "wal") {
		return fmt.Errorf("journal_mode=%q, want wal", jm)
	}
	fmt.Printf("journal_mode=%s\n", jm)
	return nil
}

func insertRows(ex interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, n int, tag string) error {
	ctx := context.Background()
	buf := make([]byte, blobBytes)
	for i := 0; i < n; i++ {
		if _, err := rand.Read(buf); err != nil {
			return err
		}
		if _, err := ex.ExecContext(ctx, "INSERT INTO t(tag, payload) VALUES(?, ?)", tag, buf); err != nil {
			return fmt.Errorf("insert %d: %w", i, err)
		}
	}
	return nil
}

// ---------- seed / write ----------

func cmdSeed(args []string) error {
	fs := flag.NewFlagSet("seed", flag.ExitOnError)
	dbPath := fs.String("db", "", "db path")
	rows := fs.Int("rows", 10, "baseline rows")
	fs.Parse(args)
	db, err := open(*dbPath, busyDefaultMs)
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.Exec("CREATE TABLE IF NOT EXISTS t(id INTEGER PRIMARY KEY, tag TEXT NOT NULL, payload BLOB)"); err != nil {
		return err
	}
	if err := assertWAL(db); err != nil {
		return err
	}
	return commitRows(db, *rows, "seed")
}

func cmdWrite(args []string) error {
	fs := flag.NewFlagSet("write", flag.ExitOnError)
	dbPath := fs.String("db", "", "db path")
	rows := fs.Int("rows", 5, "rows to commit")
	tag := fs.String("tag", "committed", "row tag")
	fs.Parse(args)
	db, err := open(*dbPath, busyDefaultMs)
	if err != nil {
		return err
	}
	defer db.Close()
	if err := assertWAL(db); err != nil {
		return err
	}
	return commitRows(db, *rows, *tag)
}

// commitRows uses an explicit BEGIN IMMEDIATE on a pinned connection.
func commitRows(db *sql.DB, n int, tag string) error {
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("BEGIN IMMEDIATE: %w", err)
	}
	if err := insertRows(conn, n, tag); err != nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return err
	}
	if _, err := conn.ExecContext(ctx, "COMMIT"); err != nil {
		return fmt.Errorf("COMMIT: %w", err)
	}
	fmt.Printf("committed %d rows tag=%s\n", n, tag)
	return nil
}

// ---------- pragmacheck: per-connection PRAGMAs on every pool connection ----------

func cmdPragmaCheck(args []string) error {
	fs := flag.NewFlagSet("pragmacheck", flag.ExitOnError)
	dbPath := fs.String("db", "", "db path")
	nconns := fs.Int("conns", 4, "pool connections to open simultaneously")
	fs.Parse(args)
	db, err := open(*dbPath, busyDefaultMs)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(*nconns)
	db.SetMaxIdleConns(*nconns)

	ctx := context.Background()
	conns := make([]*sql.Conn, 0, *nconns)
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	// Hold all connections simultaneously so the pool is FORCED to dial
	// nconns distinct driver connections — each one must have gotten the
	// DSN _pragma settings applied by modernc.org/sqlite at open time.
	for i := 0; i < *nconns; i++ {
		c, err := db.Conn(ctx)
		if err != nil {
			return fmt.Errorf("conn %d: %w", i, err)
		}
		conns = append(conns, c)
	}
	for i, c := range conns {
		bt, err := pragmaStr(c, "busy_timeout")
		if err != nil {
			return err
		}
		jm, err := pragmaStr(c, "journal_mode")
		if err != nil {
			return err
		}
		fk, err := pragmaStr(c, "foreign_keys")
		if err != nil {
			return err
		}
		fmt.Printf("conn %d: busy_timeout=%s journal_mode=%s foreign_keys=%s\n", i, bt, jm, fk)
		if bt != fmt.Sprint(busyDefaultMs) {
			return fmt.Errorf("conn %d: busy_timeout=%s, want %d — per-connection PRAGMA NOT applied", i, bt, busyDefaultMs)
		}
		if !strings.EqualFold(jm, "wal") {
			return fmt.Errorf("conn %d: journal_mode=%s, want wal", i, jm)
		}
		if fk != "1" {
			return fmt.Errorf("conn %d: foreign_keys=%s, want 1 — per-connection PRAGMA NOT applied", i, fk)
		}
	}
	fmt.Printf("all %d pool connections carry the DSN PRAGMAs\n", len(conns))
	return nil
}

// ---------- hang: the kill -9 target ----------

func cmdHang(args []string) error {
	fs := flag.NewFlagSet("hang", flag.ExitOnError)
	dbPath := fs.String("db", "", "db path")
	rows := fs.Int("rows", 20, "uncommitted rows to insert")
	tag := fs.String("tag", "uncommitted", "row tag")
	pidfile := fs.String("pidfile", "", "readiness marker + pid, written AFTER the txn holds uncommitted WAL frames")
	fs.Parse(args)
	if *pidfile == "" {
		return fmt.Errorf("--pidfile required")
	}
	// Tiny page cache so the uncommitted transaction SPILLS to the WAL file
	// before we are killed — otherwise the dirty pages sit in process memory
	// and kill -9 would prove nothing about on-disk recovery.
	db, err := open(*dbPath, busyDefaultMs, "_pragma=cache_size(-16)")
	if err != nil {
		return err
	}
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	if err := assertWAL(db); err != nil {
		return err
	}
	walPath := *dbPath + "-wal"
	walBefore := fileSize(walPath)
	if _, err := conn.ExecContext(ctx, "BEGIN IMMEDIATE"); err != nil {
		return fmt.Errorf("BEGIN IMMEDIATE: %w", err)
	}
	if err := insertRows(conn, *rows, *tag); err != nil {
		return err
	}
	walAfter := fileSize(walPath)
	if walAfter <= walBefore {
		return fmt.Errorf("WAL did not grow (before=%d after=%d) — uncommitted pages never spilled to disk, crash test would be vacuous", walBefore, walAfter)
	}
	fmt.Printf("holding open txn: %d uncommitted rows tag=%s, wal %d -> %d bytes; pid=%d\n",
		*rows, *tag, walBefore, walAfter, os.Getpid())
	if err := os.WriteFile(*pidfile, []byte(fmt.Sprintf("%d\n", os.Getpid())), 0644); err != nil {
		return err
	}
	select {} // wait to be kill -9'd
}

func fileSize(p string) int64 {
	fi, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return fi.Size()
}

// ---------- read: concurrent reader while the writer holds an open txn ----------

func cmdRead(args []string) error {
	fs := flag.NewFlagSet("read", flag.ExitOnError)
	dbPath := fs.String("db", "", "db path")
	expect := fs.Int("expect", -1, "expected committed row count")
	maxMs := fs.Int("max-ms", 2000, "max read latency (reader must not block on writer)")
	fs.Parse(args)
	db, err := open(*dbPath, busyDefaultMs)
	if err != nil {
		return err
	}
	defer db.Close()
	start := time.Now()
	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&n); err != nil {
		return err
	}
	elapsed := time.Since(start)
	fmt.Printf("reader saw %d rows in %s (writer txn open)\n", n, elapsed)
	if n != *expect {
		return fmt.Errorf("reader saw %d rows, want %d — snapshot not consistent", n, *expect)
	}
	if elapsed > time.Duration(*maxMs)*time.Millisecond {
		return fmt.Errorf("read took %s (> %dms) — reader was blocked by the writer", elapsed, *maxMs)
	}
	return nil
}

// ---------- contend: busy_timeout proof against a held write lock ----------

func cmdContend(args []string) error {
	fs := flag.NewFlagSet("contend", flag.ExitOnError)
	dbPath := fs.String("db", "", "db path")
	busyMs := fs.Int("busy-ms", 2000, "busy_timeout for this attempt")
	minMs := fs.Int("min-ms", 1500, "minimum elapsed before SQLITE_BUSY (proves the timeout waited)")
	fs.Parse(args)
	db, err := open(*dbPath, *busyMs)
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	conn, err := db.Conn(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	start := time.Now()
	_, err = conn.ExecContext(ctx, "BEGIN IMMEDIATE")
	elapsed := time.Since(start)
	if err == nil {
		conn.ExecContext(ctx, "ROLLBACK")
		return fmt.Errorf("BEGIN IMMEDIATE unexpectedly succeeded — no writer holds the lock?")
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "busy") && !strings.Contains(msg, "locked") {
		return fmt.Errorf("expected SQLITE_BUSY, got: %v", err)
	}
	fmt.Printf("second writer got %q after %s (busy_timeout=%dms)\n", err, elapsed, *busyMs)
	if elapsed < time.Duration(*minMs)*time.Millisecond {
		return fmt.Errorf("SQLITE_BUSY after only %s (< %dms) — busy_timeout not applied on this connection", elapsed, *minMs)
	}
	return nil
}

// ---------- verify: post-crash reopen ----------

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ExitOnError)
	dbPath := fs.String("db", "", "db path")
	expect := fs.Int("expect", -1, "expected committed row count")
	absentTag := fs.String("absent-tag", "uncommitted", "tag that must have zero rows")
	fs.Parse(args)
	db, err := open(*dbPath, busyDefaultMs)
	if err != nil {
		return err
	}
	defer db.Close()
	ic, err := pragmaStr(db, "integrity_check")
	if err != nil {
		return err
	}
	fmt.Printf("integrity_check=%s\n", ic)
	if ic != "ok" {
		return fmt.Errorf("integrity_check=%q, want ok", ic)
	}
	if err := assertWAL(db); err != nil {
		return err
	}
	var total, ghost int
	if err := db.QueryRow("SELECT COUNT(*) FROM t").Scan(&total); err != nil {
		return err
	}
	if err := db.QueryRow("SELECT COUNT(*) FROM t WHERE tag = ?", *absentTag).Scan(&ghost); err != nil {
		return err
	}
	fmt.Printf("rows=%d rows(tag=%s)=%d\n", total, *absentTag, ghost)
	if total != *expect {
		return fmt.Errorf("row count %d, want %d — committed rows lost or uncommitted leaked", total, *expect)
	}
	if ghost != 0 {
		return fmt.Errorf("%d rows with tag=%s survived the crash — uncommitted txn leaked", ghost, *absentTag)
	}
	return nil
}
