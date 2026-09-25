// Command stubdocker stands in for docker while the provisioning
// service is tested.
//
// It answers only the four things besy-provision asks, and records the
// peers it is told to add so the test can hand them to the AmneziaWG
// device it runs. The service under test is the real binary; only the
// container behind it is pretend.
//
// It reads what to report from the environment, so one build serves
// every case the test wants to set up.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func main() {
	args := os.Args[1:]
	if len(args) == 0 {
		os.Exit(0)
	}

	switch {
	case args[0] == "ps":
		fmt.Println(os.Getenv("STUB_CONTAINER"))

	case args[0] == "port":
		// docker port <container> <port>/udp. Silent and failing when
		// STUB_PUBLISHED is unset, as docker is for a container on the
		// host's network.
		if p := os.Getenv("STUB_PUBLISHED"); p != "" {
			fmt.Printf("0.0.0.0:%s\n", p)
			return
		}
		os.Exit(1)

	case args[0] == "exec":
		// docker exec [-i] <container> <command...>
		rest := args[1:]
		if len(rest) > 0 && rest[0] == "-i" {
			rest = rest[1:]
		}
		if len(rest) > 1 {
			command(rest[1:])
		}
	}
}

func command(argv []string) {
	joined := strings.Join(argv, " ")

	switch {
	case strings.HasPrefix(joined, "awg show all dump"):
		// The interface's own line, then whatever peers have been added.
		// The listen port is whatever the test says, because assuming
		// 51820 here is exactly how the installer's wrong guess went
		// unnoticed: the stub agreed with it.
		port := os.Getenv("STUB_LISTEN_PORT")
		if port == "" {
			port = "51820"
		}
		fmt.Printf("%s\t%s\t%s\t%s\t0\n",
			os.Getenv("STUB_IFACE"), "PRIVATE", os.Getenv("STUB_SERVER_PUBLIC"), port)
		if peers, err := os.ReadFile(os.Getenv("STUB_PEERS")); err == nil {
			os.Stdout.Write(peers)
		}

	case strings.HasPrefix(joined, "awg showconf"):
		fmt.Print(os.Getenv("STUB_SHOWCONF"))

	case strings.HasPrefix(joined, "awg set"):
		// awg set <iface> peer <key> allowed-ips <cidr>
		var key, allowed string
		for i, a := range argv {
			if a == "peer" && i+1 < len(argv) {
				key = argv[i+1]
			}
			if a == "allowed-ips" && i+1 < len(argv) {
				allowed = argv[i+1]
			}
		}
		if key == "" {
			return
		}
		f, err := os.OpenFile(os.Getenv("STUB_PEERS"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		defer f.Close()
		fmt.Fprintf(f, "%s\t%s\t(none)\t(none)\t%s\t0\t0\t0\toff\n",
			os.Getenv("STUB_IFACE"), key, allowed)

	case strings.HasPrefix(joined, "sh -c") && len(argv) >= 3:
		// Files inside the container are read and written through a
		// shell. Run it for real, so the quoting the service builds is
		// tested by an actual shell rather than taken on trust.
		cmd := exec.Command("sh", "-c", argv[2])
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		if err := cmd.Run(); err != nil {
			os.Exit(1)
		}

	case strings.HasPrefix(joined, "ip -o addr show dev"):
		fmt.Printf("5: %s    inet 10.8.1.0/24 scope global %s\\       valid_lft forever preferred_lft forever\n",
			os.Getenv("STUB_IFACE"), os.Getenv("STUB_IFACE"))
	}
}
