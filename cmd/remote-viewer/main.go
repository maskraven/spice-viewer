// Command remote-viewer is the virt-viewer product binary.
//
// Phase 1 will open Proxmox .vv files and drive a SPICE session.
// This stub only prints version and help; no protocol is implemented yet.
package main

import (
	"flag"
	"fmt"
	"os"
)

// Version is set at link time in release builds; default is development.
var Version = "dev"

func main() {
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [flags] [file.vv]\n\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "SPICE remote viewer (scaffold; pre-v0.1).\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(Version)
		return
	}

	// No connection logic yet — print help when invoked with no args.
	if flag.NArg() == 0 {
		flag.Usage()
		os.Exit(0)
	}

	fmt.Fprintf(os.Stderr, "remote-viewer: opening connection files is not implemented yet (pre-v0.1)\n")
	os.Exit(2)
}
