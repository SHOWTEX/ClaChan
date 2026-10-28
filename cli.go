package main

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"os"
)

// runCLI handles the one administrative command: resetting a profile password.
// The server itself has no password-recovery flow, so this is the escape hatch
// for a forgotten owner password. Run it on the Pi with the service stopped.
func runCLI(args []string) int {
	flags := flag.NewFlagSet("clachan", flag.ContinueOnError)
	reset := flags.String("reset-password", "", "profile name whose password should be reset")
	showVersion := flags.Bool("version", false, "print version and exit")
	if err := flags.Parse(args); err != nil {
		return 2
	}
	if *showVersion {
		fmt.Printf("clachan %s\n", version)
		return 0
	}
	if *reset == "" {
		fmt.Fprintln(os.Stderr, "usage: clachan [-version] [-reset-password <profile-name>]")
		return 2
	}

	loadDotEnv(".env")
	cfg := loadConfig()

	store, err := openStore(cfg.DBPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "open database:", err)
		return 1
	}
	defer store.Close()

	profile, err := store.profileByName(context.Background(), *reset)
	if errors.Is(err, ErrNotFound) {
		fmt.Fprintf(os.Stderr, "no profile named %q\n", *reset)
		return 1
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "look up profile:", err)
		return 1
	}

	password, err := temporaryPassword()
	if err != nil {
		fmt.Fprintln(os.Stderr, "generate password:", err)
		return 1
	}
	hash, salt, iters, err := hashPassword(password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "hash password:", err)
		return 1
	}
	if err := store.updateProfilePassword(context.Background(), profile.ID, hash, salt, iters); err != nil {
		fmt.Fprintln(os.Stderr, "save password:", err)
		return 1
	}
	if err := store.deleteSessionsForProfile(context.Background(), profile.ID); err != nil {
		fmt.Fprintln(os.Stderr, "sign out sessions:", err)
		return 1
	}

	fmt.Printf("New password for %q: %s\n", profile.Name, password)
	fmt.Println("All existing sessions for that profile were signed out.")
	fmt.Println("Sign in and change it from the profile dialog.")
	return 0
}

func temporaryPassword() (string, error) {
	raw := make([]byte, 12)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}
