// Command denynetlink runs a program the way Android 11 runs an app's
// processes as far as netlink is concerned: bind() on a netlink socket
// is refused with EACCES, and everything else is left alone.
//
// Android 11 stopped letting ordinary apps bind NETLINK_ROUTE sockets.
// A desktop kernel allows it, which is how an engine that does it passed
// every test here and then failed on the first phone that ran it. This
// puts the phone's rule on this machine.
//
// The filter cannot look inside the address bind() is given — seccomp
// sees only the raw arguments — but it can see the address length, and
// a netlink address is 12 bytes where IPv4 is 16 and IPv6 is 28. So a
// 12-byte bind is refused and nothing else is touched.
//
//	denynetlink program [args...]
package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

const (
	ldAbs = unix.BPF_LD | unix.BPF_W | unix.BPF_ABS
	jeqK  = unix.BPF_JMP | unix.BPF_JEQ | unix.BPF_K
	retK  = unix.BPF_RET | unix.BPF_K

	auditArchX8664 = 0xc000003e
	sysBind        = 49 // x86_64
	sockaddrNLSize = 12

	offNR   = 0
	offArch = 4
	offArg2 = 16 + 2*8 // low half, little-endian
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: denynetlink program [args...]")
		os.Exit(2)
	}
	if runtime.GOARCH != "amd64" {
		fmt.Fprintln(os.Stderr, "denynetlink: the filter is written for x86_64")
		os.Exit(2)
	}

	filter := []unix.SockFilter{
		{Code: ldAbs, K: offArch},
		{Code: jeqK, K: auditArchX8664, Jt: 0, Jf: 4},
		{Code: ldAbs, K: offNR},
		{Code: jeqK, K: sysBind, Jt: 0, Jf: 2},
		{Code: ldAbs, K: offArg2},
		{Code: jeqK, K: sockaddrNLSize, Jt: 1, Jf: 0},
		{Code: retK, K: unix.SECCOMP_RET_ALLOW},
		{Code: retK, K: unix.SECCOMP_RET_ERRNO | uint32(unix.EACCES)},
	}
	prog := unix.SockFprog{Len: uint16(len(filter)), Filter: &filter[0]}

	// The filter belongs to the thread that installs it and survives
	// exec, so install and exec from one thread.
	runtime.LockOSThread()
	if err := unix.Prctl(unix.PR_SET_NO_NEW_PRIVS, 1, 0, 0, 0); err != nil {
		fmt.Fprintln(os.Stderr, "denynetlink: no_new_privs:", err)
		os.Exit(1)
	}
	if err := unix.Prctl(unix.PR_SET_SECCOMP, unix.SECCOMP_MODE_FILTER,
		uintptr(unsafe.Pointer(&prog)), 0, 0); err != nil {
		fmt.Fprintln(os.Stderr, "denynetlink: seccomp:", err)
		os.Exit(1)
	}

	path, err := exec.LookPath(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "denynetlink:", err)
		os.Exit(1)
	}
	err = syscall.Exec(path, os.Args[1:], os.Environ())
	fmt.Fprintln(os.Stderr, "denynetlink: exec:", err)
	os.Exit(1)
}
