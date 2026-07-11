// S1 setuid-gate probe. Throwaway spike code (specs/implementation-handoff.md Part 2).
//
// One static binary, installed twice in the image:
//   /usr/local/bin/mc         owned by the privileged uid, mode 4755 (setuid)
//   /usr/local/bin/agentprobe owned by root, mode 0755 (NO setuid)
//
// Subcommands:
//   init <db>        create the spine DB (WAL) with one sentinel row
//   read <db>        open via modernc.org/sqlite and SELECT the sentinel row
//   write <db>       INSERT a row through sqlite (proves the gate brokers writes too)
//   directopen <db>  raw open(2) O_RDONLY; exits 0 IFF the kernel refuses with EACCES
//   directwrite <db> raw open(2) O_RDWR;   exits 0 IFF the kernel refuses with EACCES
//   ids              print ruid/euid only
package main

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"syscall"

	_ "modernc.org/sqlite"
)

func ids() string {
	return fmt.Sprintf("ruid=%d euid=%d", syscall.Getuid(), syscall.Geteuid())
}

func openDB(path string) (*sql.DB, error) {
	// busy_timeout per connection; journal mode is set once at init.
	dsn := "file:" + path + "?_pragma=busy_timeout(5000)"
	return sql.Open("sqlite", dsn)
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "FAIL: "+format+"\n", a...)
	os.Exit(1)
}

func directOpen(path string, flags int, label string) {
	fmt.Printf("%s: %s attempting raw open(2) %s\n", label, ids(), path)
	fd, err := syscall.Open(path, flags, 0)
	if err == nil {
		syscall.Close(fd)
		fail("%s: open() SUCCEEDED — the kernel gate is NOT holding", label)
	}
	var errno syscall.Errno
	if errors.As(err, &errno) && errno == syscall.EACCES {
		fmt.Printf("%s: OK — kernel refused with EACCES\n", label)
		return
	}
	fail("%s: refused, but with %v (want EACCES)", label, err)
}

func main() {
	if len(os.Args) < 2 {
		fail("usage: %s init|read|write|directopen|directwrite|ids [db-path]", os.Args[0])
	}
	cmd := os.Args[1]
	if cmd == "ids" {
		fmt.Println(ids())
		return
	}
	if len(os.Args) < 3 {
		fail("subcommand %q needs a db path", cmd)
	}
	path := os.Args[2]

	switch cmd {
	case "init":
		db, err := openDB(path)
		if err != nil {
			fail("open: %v", err)
		}
		defer db.Close()
		stmts := []string{
			"PRAGMA journal_mode=WAL",
			"CREATE TABLE IF NOT EXISTS spine (id INTEGER PRIMARY KEY, v TEXT NOT NULL)",
			"INSERT INTO spine (v) VALUES ('hello-from-spine')",
		}
		for _, s := range stmts {
			if _, err := db.Exec(s); err != nil {
				fail("init %q: %v", s, err)
			}
		}
		fmt.Printf("init: %s created %s (WAL, 1 row)\n", ids(), path)

	case "read":
		fmt.Printf("read: %s\n", ids())
		db, err := openDB(path)
		if err != nil {
			fail("open: %v", err)
		}
		defer db.Close()
		var v string
		if err := db.QueryRow("SELECT v FROM spine ORDER BY id LIMIT 1").Scan(&v); err != nil {
			fail("select: %v", err)
		}
		fmt.Printf("read: OK — sentinel row = %q\n", v)

	case "write":
		fmt.Printf("write: %s\n", ids())
		db, err := openDB(path)
		if err != nil {
			fail("open: %v", err)
		}
		defer db.Close()
		if _, err := db.Exec("INSERT INTO spine (v) VALUES ('written-through-gate')"); err != nil {
			fail("insert: %v", err)
		}
		fmt.Println("write: OK — row inserted through the gate")

	case "directopen":
		directOpen(path, syscall.O_RDONLY, "directopen")

	case "directwrite":
		directOpen(path, syscall.O_RDWR, "directwrite")

	default:
		fail("unknown subcommand %q", cmd)
	}
}
