// mc_dispatch is the public, unprivileged mc entrypoint baked into the agent
// image. Only a Worker completion carrying --seal-request crosses to the
// setuid wrapper; every other verb retains the calling model/runner uid.
package main

import (
	"os"
	"syscall"
)

func main() {
	target := "/usr/local/libexec/mc-real"
	for _, arg := range os.Args[1:] {
		if arg == "--seal-request" {
			target = "/usr/local/libexec/mc-complete"
			break
		}
	}
	args := append([]string{target}, os.Args[1:]...)
	if err := syscall.Exec(target, args, os.Environ()); err != nil {
		_, _ = os.Stderr.WriteString("mc: exec wrapper: " + err.Error() + "\n")
		os.Exit(127)
	}
}
