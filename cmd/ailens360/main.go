package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/CoolBanHub/ailens360/internal/app"
	"github.com/CoolBanHub/ailens360/internal/config"
	"github.com/CoolBanHub/ailens360/internal/crypto"
	"github.com/CoolBanHub/ailens360/internal/version"
)

const usage = `AILens360 — 360° observability for every LLM call.

Usage:
  ailens360 server [--env path]   Start the HTTP server (env path defaults to .env)
  ailens360 keygen                Generate a base64 32-byte master key
  ailens360 version               Print version and exit
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "server":
		os.Exit(runServer(os.Args[2:]))
	case "keygen":
		k, err := crypto.GenerateKey()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		fmt.Println(k)
	case "version", "-v", "--version":
		fmt.Printf("ailens360 %s (commit=%s build=%s)\n", version.Version, version.Commit, version.BuildTime)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

func runServer(args []string) int {
	fs := flag.NewFlagSet("server", flag.ExitOnError)
	envPath := fs.String("env", ".env", "path to .env file (optional; ignored if missing)")
	_ = fs.Parse(args)

	cfg, err := config.Load(*envPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		return 1
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintln(os.Stderr, "config invalid:", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	a, err := app.Build(ctx, cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "build app:", err)
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	errCh := make(chan error, 1)
	go func() { errCh <- a.Run() }()

	var runErr error
	select {
	case sig := <-sigCh:
		a.Logger.Info("signal received", "sig", sig.String())
	case runErr = <-errCh:
		if runErr != nil {
			a.Logger.Error("http server exited", "err", runErr)
		}
	}
	a.Shutdown(context.Background())
	if runErr != nil {
		// Surface bind failures and unexpected ListenAndServe errors to the
		// process supervisor so failed starts aren't reported as success.
		return 1
	}
	return 0
}
