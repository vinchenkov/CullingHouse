//go:build !test_fake_routing

package main

import (
	"strings"
	"testing"
)

func TestBoundedDoctorBufferRetainsPrefixWithoutBlockingChild(t *testing.T) {
	b := boundedDoctorBuffer{limit: 8}
	payload := strings.Repeat("x", 32)
	n, err := b.Write([]byte(payload))
	if err != nil || n != len(payload) {
		t.Fatalf("write = %d, %v", n, err)
	}
	if b.String() != strings.Repeat("x", 8) || !b.overflow {
		t.Fatalf("buffer = %q overflow=%v", b.String(), b.overflow)
	}
}
