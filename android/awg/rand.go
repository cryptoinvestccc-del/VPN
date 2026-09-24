package main

import (
	"crypto/rand"
	"io"
)

// randSource is the only source of randomness here, named so the one
// place it comes from is obvious: the kernel, never a seeded generator.
func randSource() io.Reader { return rand.Reader }
