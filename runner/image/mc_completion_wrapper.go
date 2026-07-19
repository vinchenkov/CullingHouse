// mc_completion_wrapper is the only setuid binary in the pipeline image. It
// admits exactly `mc complete <task> --run <run> --seal-request <16-hex>`,
// permanently drops to the image's completion uid, installs the fixed
// lock-domain environment, and execs the real mc binary. It never exposes a
// general privileged mc surface to the model uid.
package main

import (
	"os"
	"regexp"
	"strconv"
	"syscall"
)

const completionUID = 10001

var completionRequest = regexp.MustCompile(`^[0-9a-f]{16}$`)

func reject(reason string) {
	_, _ = os.Stderr.WriteString("mc: sealed completion wrapper refused " + reason + "\n")
	os.Exit(2)
}

func main() {
	args := os.Args
	if os.Geteuid() != completionUID {
		reject("its identity")
	}
	if len(args) != 7 || args[1] != "complete" || args[3] != "--run" || args[4] == "" ||
		args[5] != "--seal-request" || !completionRequest.MatchString(args[6]) {
		reject("its arguments")
	}
	if task, err := strconv.ParseInt(args[2], 10, 64); err != nil || task < 1 {
		reject("its task id")
	}
	// Drop saved ids as well: after this point the real Go binary has only the
	// completion uid, not root or the calling model uid.
	if err := syscall.Setresgid(completionUID, completionUID, completionUID); err != nil {
		reject("its group transition")
	}
	if err := syscall.Setresuid(completionUID, completionUID, completionUID); err != nil {
		reject("its uid transition")
	}
	os.Clearenv()
	os.Setenv("PATH", "/usr/bin:/bin")
	os.Setenv("MC_SPINE", "/mc/spine/spine.db")
	os.Setenv("MC_RUN_JSON", "/mc/run.json")
	target := "/usr/local/libexec/mc-real"
	if err := syscall.Exec(target, append([]string{target}, args[1:]...), os.Environ()); err != nil {
		_, _ = os.Stderr.WriteString("mc: exec completion: " + err.Error() + "\n")
		os.Exit(127)
	}
}
