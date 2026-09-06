package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"

	"github.com/rackops/rackops/internal/server"
)

func runRecoverAccount(args []string) {
	flags := flag.NewFlagSet("recover-account", flag.ExitOnError)
	source := flags.String("snapshot", "", "encrypted control-plane snapshot or state.json")
	keyPath := flags.String("key", "", "original state encryption key file")
	target := flags.String("data-dir", "", "new, nonexistent recovery directory")
	email := flags.String("email", "", "existing account to recover")
	stdin := flags.Bool("password-stdin", false, "read the replacement password from standard input")
	flags.Parse(args)
	if *source == "" || *keyPath == "" || *target == "" || *email == "" || !*stdin {
		log.Fatal("recover-account requires --snapshot, --key, --data-dir, --email and --password-stdin")
	}
	password, err := io.ReadAll(io.LimitReader(os.Stdin, 1024))
	if err != nil {
		log.Fatal("could not read password")
	}
	secret := strings.TrimSuffix(strings.TrimSuffix(string(password), "\n"), "\r")
	if len(secret) < 8 || len(secret) > 72 {
		log.Fatal("password must contain 8 to 72 bytes")
	}
	sealed, err := os.ReadFile(*source)
	if err != nil {
		log.Fatal(err)
	}
	key, err := os.ReadFile(*keyPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := server.RecoverAccountSnapshot(*target, sealed, key, *email, secret); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Recovered state written to the new directory. All prior sessions and this account's API keys are invalidated. Stop the original control plane before using the recovered state. Restore loose identity files separately; application data was not restored.")
}
