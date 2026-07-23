//go:build test_fake_routing

package main

import "io"

func brokerOnboardHome(stdout, stderr io.Writer) int {
	return runLocal([]string{"onboard", "home"}, nil, stdout, stderr)
}

func brokerOnboardContainer(args []string, stdout, stderr io.Writer) int {
	return runLocal(args, nil, stdout, stderr)
}

func brokerOnboardRuntimeAuth(args []string, stdout, stderr io.Writer) int {
	return runLocal(args, nil, stdout, stderr)
}

func brokerOnboardVerify(args []string, stdout, stderr io.Writer) int {
	return runLocal(args, nil, stdout, stderr)
}
