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
	"github.com/CoolBanHub/ailens360/internal/version"
)

const usage = `AILens360 — 360° observability for every LLM call.

Usage:
  ailens360 proxy [--env path]      Start the reverse-proxy process
  ailens360 collector [--env path]  Start the trace-collector process
  ailens360 api [--env path]        Start the REST/UI api process
  ailens360 version                 Print version and exit

Each process role consumes the same env file but only requires the config
sections it actually touches.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	switch os.Args[1] {
	case "proxy":
		os.Exit(runRole(os.Args[2:], config.RoleProxy))
	case "collector":
		os.Exit(runRole(os.Args[2:], config.RoleCollector))
	case "api":
		os.Exit(runRole(os.Args[2:], config.RoleAPI))
	case "version", "-v", "--version":
		fmt.Printf("ailens360 %s (commit=%s build=%s)\n", version.Version, version.Commit, version.BuildTime)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
}

// runner is the small surface every role exposes — main is intentionally
// uniform across roles so adding a new one is a five-line change.
type runner interface {
	Run() <-chan error
	Shutdown(context.Context)
}

func runRole(args []string, role config.Role) int {
	fs := flag.NewFlagSet(string(role), flag.ExitOnError)
	envPath := fs.String("env", ".env", "path to .env file (optional; ignored if missing)")
	_ = fs.Parse(args)

	cfg, err := config.Load(*envPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "load config:", err)
		return 1
	}
	if err := cfg.Validate(role); err != nil {
		fmt.Fprintln(os.Stderr, "config invalid:", err)
		return 1
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var r runner
	switch role {
	case config.RoleProxy:
		p, err := app.BuildProxy(ctx, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "build proxy:", err)
			return 1
		}
		r = p
	case config.RoleCollector:
		c, err := app.BuildCollector(ctx, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "build collector:", err)
			return 1
		}
		r = c
	case config.RoleAPI:
		a, err := app.BuildAPI(ctx, cfg)
		if err != nil {
			fmt.Fprintln(os.Stderr, "build api:", err)
			return 1
		}
		r = a
	default:
		fmt.Fprintln(os.Stderr, "unknown role:", role)
		return 1
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	errCh := r.Run()

	var runErr error
	select {
	case sig := <-sigCh:
		fmt.Fprintln(os.Stderr, "signal received:", sig.String())
	case runErr = <-errCh:
		if runErr != nil {
			fmt.Fprintln(os.Stderr, "server exited:", runErr)
		}
	}
	r.Shutdown(context.Background())
	if runErr != nil {
		return 1
	}
	return 0
}
