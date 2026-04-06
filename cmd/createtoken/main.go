// createtoken is a one-shot CLI tool for bootstrapping the first admin auth
// token.  It reads DB credentials from .env (same file as the main server),
// generates a new token, persists it, and prints the raw hex token to stdout.
//
// Usage:
//
//	go run ./cmd/createtoken --name=root --clearance=1000
package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/sebun1/steamPlaytimeTracker/sptt"
)

func main() {
	name      := flag.String("name", "", "unique token name (required)")
	clearance := flag.Int("clearance", 0, "clearance level for this token (required, > 0)")
	envFile   := flag.String("env", ".env", "path to .env file")
	flag.Parse()

	if *name == "" {
		fmt.Fprintln(os.Stderr, "error: --name is required")
		os.Exit(1)
	}
	if *clearance <= 0 {
		fmt.Fprintln(os.Stderr, "error: --clearance must be > 0")
		os.Exit(1)
	}

	env, err := sptt.GetEnv(*envFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error reading %s: %v\n", *envFile, err)
		os.Exit(1)
	}

	for _, key := range []string{"DB_USER", "DB_PASSWORD", "DB_NAME"} {
		if _, ok := env[key]; !ok {
			fmt.Fprintf(os.Stderr, "error: %s not set in %s\n", key, *envFile)
			os.Exit(1)
		}
	}

	db, err := sptt.NewDB(env["DB_USER"], env["DB_PASSWORD"], env["DB_NAME"])
	if err != nil {
		fmt.Fprintf(os.Stderr, "error connecting to database: %v\n", err)
		os.Exit(1)
	}
	defer db.Close()

	tokenHex, saltHex, secretHex, err := sptt.GenerateToken()
	if err != nil {
		fmt.Fprintf(os.Stderr, "error generating token: %v\n", err)
		os.Exit(1)
	}

	ctx := context.Background()
	if err := db.CreateAuthToken(ctx, *name, saltHex, secretHex, *clearance); err != nil {
		fmt.Fprintf(os.Stderr, "error storing token: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(tokenHex)
}
