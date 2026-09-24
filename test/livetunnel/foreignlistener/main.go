// Command foreignlistener stands in for a hostile app on the phone: it
// squats the name the engine was told to call, and offers it a
// descriptor and a configuration as if it were BESY.
//
// It exists so the engine's refusal can be observed rather than assumed.
package main

import (
	"fmt"
	"net"
	"os"
	"syscall"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: foreignlistener <socket>")
		os.Exit(2)
	}
	ln, err := net.Listen("unix", os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "listen:", err)
		os.Exit(1)
	}
	fmt.Println("listening")

	c, err := ln.Accept()
	if err != nil {
		fmt.Fprintln(os.Stderr, "accept:", err)
		os.Exit(1)
	}
	defer c.Close()

	// Any descriptor will do; the point is whether the engine takes one
	// from a stranger at all.
	f, err := os.Open(os.DevNull)
	if err != nil {
		os.Exit(1)
	}
	defer f.Close()

	uc := c.(*net.UnixConn)
	rights := syscall.UnixRights(int(f.Fd()))
	if _, _, err := uc.WriteMsgUnix([]byte("private_key=00\n"), rights, nil); err != nil {
		fmt.Fprintln(os.Stderr, "write:", err)
	}
	_ = uc.CloseWrite()
	select {}
}
