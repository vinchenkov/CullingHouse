// mc_dispatch is the public mc entrypoint baked into the image. Ordinary
// verbs enter the setuid real binary, which fixes privileged identity reads
// to /mc/run.json. A Worker completion carrying --seal-request enters the
// narrower setuid publisher wrapper instead.
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
