// Command obfsctl manages the per-client credential file: adding a
// client, revoking one, and listing who has access.
//
// Editing the file by hand works too — it is plain YAML — but generating
// a key by hand invites the two mistakes that matter: a key with too
// little entropy, and a key accidentally shared between two devices,
// which quietly destroys the ability to revoke either one.
package main

import (
	"crypto/rand"
	"encoding/base64"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/cryptoinvestccc-del/vpn/internal/clients"
)

func main() {
	log := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	flag.Usage = func() {
		fmt.Fprint(os.Stderr, `obfsctl manages the per-client credential file.

Usage:
  obfsctl -file clients.yaml add <client-id>     generate a credential
  obfsctl -file clients.yaml revoke <client-id>  withdraw access
  obfsctl -file clients.yaml restore <client-id> give it back
  obfsctl -file clients.yaml list                show who has access
  obfsctl -file clients.yaml profile <client-id> -wg <client.conf> -endpoint <host:port>
                                                 build one file the client can import

After add, revoke or restore, tell a running server to pick up the
change with: systemctl reload obfsserver  (or: kill -HUP <pid>)

Flags:
`)
		flag.PrintDefaults()
	}

	path := flag.String("file", "clients.yaml", "path to the credential file")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	file, err := readFile(*path)
	if err != nil {
		log("obfsctl: %v", err)
		os.Exit(1)
	}

	switch args[0] {
	case "add":
		if len(args) != 2 {
			log("obfsctl: add needs exactly one client id")
			os.Exit(2)
		}
		if err := addClient(file, args[1]); err != nil {
			log("obfsctl: %v", err)
			os.Exit(1)
		}
		if err := writeFile(*path, file); err != nil {
			log("obfsctl: %v", err)
			os.Exit(1)
		}

	case "revoke", "restore":
		if len(args) != 2 {
			log("obfsctl: %s needs exactly one client id", args[0])
			os.Exit(2)
		}
		if err := setDisabled(file, args[1], args[0] == "revoke"); err != nil {
			log("obfsctl: %v", err)
			os.Exit(1)
		}
		if err := writeFile(*path, file); err != nil {
			log("obfsctl: %v", err)
			os.Exit(1)
		}
		fmt.Printf("%s: %s\n", args[1], map[bool]string{true: "revoked", false: "restored"}[args[0] == "revoke"])
		fmt.Println("\nReload the server to apply: systemctl reload obfsserver")

	case "profile":
		if err := profileCommand(file, args[1:]); err != nil {
			log("obfsctl: %v", err)
			os.Exit(1)
		}

	case "list":
		listClients(file)

	default:
		flag.Usage()
		os.Exit(2)
	}
}

func readFile(path string) (*clients.File, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return &clients.File{}, nil
	}
	if err != nil {
		return nil, err
	}

	var file clients.File
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	return &file, nil
}

// writeFile replaces the credential file atomically and with restrictive
// permissions: it holds every client's key, and a half-written one would
// lock everybody out.
func writeFile(path string, file *clients.File) error {
	data, err := yaml.Marshal(file)
	if err != nil {
		return err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".clients-*.yaml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())

	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

func addClient(file *clients.File, id string) error {
	if id == "" {
		return fmt.Errorf("a client needs an id")
	}
	for _, c := range file.Clients {
		if c.ID == id {
			return fmt.Errorf("client %q already exists; revoke it or pick another id", id)
		}
	}

	var key [32]byte
	if _, err := rand.Read(key[:]); err != nil {
		return err
	}
	psk := base64.StdEncoding.EncodeToString(key[:])

	file.Clients = append(file.Clients, clients.Client{ID: id, PSKBase64: psk})

	fmt.Printf("Added client %q.\n\nPut this in that client's config, and nowhere else:\n\n", id)
	fmt.Printf("psk: %q\n\n", psk)
	fmt.Println("Each client must have its own key. Two devices sharing one means")
	fmt.Println("neither can be revoked without cutting off the other.")
	fmt.Println("\nReload the server to apply: systemctl reload obfsserver")
	return nil
}

func setDisabled(file *clients.File, id string, disabled bool) error {
	for i := range file.Clients {
		if file.Clients[i].ID == id {
			file.Clients[i].Disabled = disabled
			return nil
		}
	}
	return fmt.Errorf("no client named %q", id)
}

func listClients(file *clients.File) {
	if len(file.Clients) == 0 {
		fmt.Println("No clients configured.")
		return
	}
	for _, c := range file.Clients {
		status := "enabled"
		if c.Disabled {
			status = "revoked"
		}
		fmt.Printf("%-24s %s\n", c.ID, status)
	}
}
