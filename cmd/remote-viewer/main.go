// Copyright 2026 The virt-viewer authors.
// SPDX-License-Identifier: Apache-2.0

// Command remote-viewer is the virt-viewer product binary.
//
// It opens a Proxmox/virt-viewer .vv connection file, establishes a SPICE
// session via pkg/spice, and presents either a Fyne GUI (default) or a
// headless NullDriver session (--headless). Product parse always honors
// delete-this-file=1.
//
// Import rules: only pkg/*, internal/ui, and internal/ux (see scripts/check_imports.sh).
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/maskraven/virt-viewer/internal/ui"
	"github.com/maskraven/virt-viewer/internal/ux"
	"github.com/maskraven/virt-viewer/pkg/spice"
	"github.com/maskraven/virt-viewer/pkg/vvfile"
)

// Version is set at link time in release builds; default is development.
var Version = "dev"

// exit codes
const (
	exitOK    = 0
	exitFail  = 1
	exitUsage = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

// options holds parsed CLI flags and the positional .vv path.
type options struct {
	version  bool
	headless bool
	// shareDir is an optional host directory for the WebDAV share scaffold.
	shareDir string
	// path is the .vv file; empty when no positional was given.
	path string
}

// parseArgs parses flags from args (excluding the program name).
// On help (-h / -help), returns err == flag.ErrHelp.
func parseArgs(args []string, stderr io.Writer) (options, error) {
	var opts options
	fs := flag.NewFlagSet("remote-viewer", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.BoolVar(&opts.version, "version", false, "print version and exit")
	fs.BoolVar(&opts.headless, "headless", false, "run without GUI (NullDriver; for CI and dogfood)")
	fs.StringVar(&opts.shareDir, "share-dir", "", "optional host directory for WebDAV share scaffold (best-effort)")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "Usage: remote-viewer [flags] <file.vv>\n\n")
		fmt.Fprintf(stderr, "Open a virt-viewer / Proxmox SPICE connection file and establish a session.\n\n")
		fmt.Fprintf(stderr, "GUI is the default. Use --headless for NullDriver (CI / no display).\n")
		fmt.Fprintf(stderr, "By default the connection file is deleted after parse when it sets\n")
		fmt.Fprintf(stderr, "delete-this-file=1 (virt-viewer product semantics).\n\n")
		fmt.Fprintf(stderr, "Flags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nExamples:\n")
		fmt.Fprintf(stderr, "  remote-viewer pve-spice.vv\n")
		fmt.Fprintf(stderr, "  remote-viewer --headless pve-spice.vv\n")
		fmt.Fprintf(stderr, "  remote-viewer --share-dir=$HOME/Public pve-spice.vv\n")
		fmt.Fprintf(stderr, "  remote-viewer -version\n")
	}
	if err := fs.Parse(args); err != nil {
		return opts, err
	}
	rest := fs.Args()
	if len(rest) > 1 {
		return opts, fmt.Errorf("too many arguments: expected at most one .vv path")
	}
	if len(rest) == 1 {
		opts.path = rest[0]
	}
	return opts, nil
}

// run is the testable entrypoint. Returns a process exit code.
func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseArgs(args, stderr)
	if err == flag.ErrHelp {
		return exitOK
	}
	if err != nil {
		fmt.Fprintf(stderr, "remote-viewer: %v\n", err)
		return exitUsage
	}

	if opts.version {
		fmt.Fprintln(stdout, Version)
		return exitOK
	}

	if opts.path == "" {
		// No args: print help (matches scaffold behavior).
		_, _ = parseArgs([]string{"-h"}, stderr)
		return exitOK
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	var runErr error
	if opts.headless {
		runErr = runHeadless(ctx, opts.path, opts.shareDir, stdout, stderr)
	} else {
		runErr = runGUI(ctx, opts.path, opts.shareDir, stdout, stderr)
	}
	if runErr != nil {
		msg := ux.UserMessage(runErr)
		if msg == "" {
			msg = runErr.Error()
		}
		fmt.Fprintf(stderr, "remote-viewer: %s\n", msg)
		if e := ux.Classify(runErr); e != nil && e.Err != nil {
			fmt.Fprintf(stderr, "remote-viewer: detail: %v\n", e.Err)
		}
		return exitFail
	}
	return exitOK
}

// runGUI parses the .vv and runs the Fyne UI (display + grab + hotkeys).
func runGUI(ctx context.Context, path, shareDir string, stdout, stderr io.Writer) error {
	f, err := openConnectionFile(path)
	if err != nil {
		return err
	}
	defer wipeBytes(f.Password)
	defer wipeBytes(f.CA)

	if f.DeleteErr != nil {
		fmt.Fprintf(stderr, "remote-viewer: warning: could not delete connection file: %v\n", f.DeleteErr)
	}

	cfg, err := spice.ConnectConfigFromVV(f)
	if err != nil {
		return err
	}
	cfg.ShareDir = shareDir

	// Title fallback for empty .vv title.
	if cfg.Title == "" {
		cfg.Title = "remote-viewer"
	}

	if title := cfg.Title; title != "" {
		fmt.Fprintf(stdout, "remote-viewer: opening GUI session (%s)\n", title)
	}

	err = ui.RunGUI(ctx, cfg)
	wipeBytes(cfg.Password)
	wipeBytes(cfg.CACertPEM)
	wipeBytes(f.Password)
	wipeBytes(f.CA)
	if err != nil {
		// Context cancel from signal: treat as clean exit.
		if ctx.Err() != nil && (err == context.Canceled || err == context.DeadlineExceeded) {
			return nil
		}
		return err
	}
	return nil
}

// runHeadless parses the .vv (with product delete semantics), connects with
// NullDriver, and waits until the session ends or ctx is cancelled.
func runHeadless(ctx context.Context, path, shareDir string, stdout, stderr io.Writer) error {
	f, err := openConnectionFile(path)
	if err != nil {
		return err
	}
	defer wipeBytes(f.Password)
	defer wipeBytes(f.CA)

	if f.DeleteErr != nil {
		fmt.Fprintf(stderr, "remote-viewer: warning: could not delete connection file: %v\n", f.DeleteErr)
	}

	cfg, err := spice.ConnectConfigFromVV(f)
	if err != nil {
		return err
	}
	cfg.ShareDir = shareDir
	// Product headless path: NullDriver, null cursor, NullPlayback, NullRecord.
	cfg.Drivers = spice.Drivers{
		Display:  spice.NewNullDriver(),
		Cursor:   spice.NewNullCursorDriver(),
		Playback: spice.NewNullPlayback(),
		Record:   spice.NewNullRecord(),
	}

	client, err := spice.Connect(ctx, cfg)
	// Caller-owned copies are no longer needed after Connect.
	wipeBytes(cfg.Password)
	wipeBytes(cfg.CACertPEM)
	wipeBytes(f.Password)
	wipeBytes(f.CA)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()

	if title := client.Title(); title != "" {
		fmt.Fprintf(stdout, "remote-viewer: connected (%s)\n", title)
	} else {
		fmt.Fprintf(stdout, "remote-viewer: connected\n")
	}

	// Wait drains Events until disconnect or ctx cancel.
	waitErr := client.Wait(ctx)
	if waitErr != nil {
		if ctx.Err() != nil && (waitErr == context.Canceled || waitErr == context.DeadlineExceeded) {
			return nil
		}
		return waitErr
	}
	return nil
}

// openConnectionFile loads path with product DeleteIfRequested semantics and
// maps parse failures to classified ux errors for stable CLI messages.
func openConnectionFile(path string) (*vvfile.File, error) {
	f, err := vvfile.ParseFile(path, vvfile.ParseOptions{
		// Product binary always honors delete-this-file=1 (library default is false).
		DeleteIfRequested: true,
	})
	if err != nil {
		return nil, mapVVError(err)
	}
	return f, nil
}

// mapVVError turns vvfile parse/open errors into *ux.Error with stable messages.
//
// Plain os/syscall errors (e.g. ENOENT) can satisfy net.Error and would be
// mis-classified as Transport by ux.Classify; check vvfile shapes first.
// Already-classified *ux.Error values are preserved.
func mapVVError(err error) error {
	if err == nil {
		return nil
	}
	var uxErr *ux.Error
	if errors.As(err, &uxErr) {
		return uxErr
	}
	msg := err.Error()
	switch {
	case strings.Contains(msg, "type must be spice"),
		strings.Contains(msg, "Not a SPICE"):
		return ux.New(ux.ClassConfig, ux.MsgConfigNotSpice, err)
	case strings.Contains(msg, "exceeds max"),
		strings.Contains(msg, "exceeds protocol limit"),
		strings.Contains(msg, "field too large"):
		return ux.New(ux.ClassConfig, ux.MsgConfigFieldTooLarge, err)
	case strings.Contains(msg, "vvfile:"):
		return ux.New(ux.ClassConfig, ux.MsgConfigEndpoint, err)
	default:
		return ux.Classify(err)
	}
}

// wipeBytes zeros b in place (best-effort secret cleanup without importing
// internal/security, which cmd may not import).
func wipeBytes(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
