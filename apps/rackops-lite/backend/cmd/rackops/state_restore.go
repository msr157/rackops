package main

import (
	"flag"
	"fmt"
	"github.com/rackops/rackops/internal/server"
	"log"
	"os"
)

func runStateRestore(args []string) {
	flags := flag.NewFlagSet("restore-state", flag.ExitOnError)
	snapshot := flags.String("snapshot", "", "encrypted snapshot file")
	keyPath := flags.String("key", "", "original 32-byte state key file")
	target := flags.String("data-dir", "", "new, nonexistent destination directory")
	flags.Parse(args)
	if *snapshot == "" || *keyPath == "" || *target == "" {
		log.Fatal("restore-state requires --snapshot, --key and --data-dir")
	}
	sealed, err := os.ReadFile(*snapshot)
	if err != nil {
		log.Fatal(err)
	}
	key, err := os.ReadFile(*keyPath)
	if err != nil {
		log.Fatal(err)
	}
	if err := server.RestoreStateSnapshot(*target, sealed, key); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Control-plane state restored. Restore enrollment and mesh identity files separately before reconnecting agents. App volume and database data are not in this snapshot.")
}
