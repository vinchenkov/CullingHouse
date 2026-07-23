//go:build test_fake_routing

package main

import "io"

func brokerOnboardHome(stdout, stderr io.Writer) int {
	return runLocal([]string{"onboard", "home"}, nil, stdout, stderr)
}
