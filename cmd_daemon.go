package main

// Subcommands for the frontmatter daemon and .base evaluation. They live in
// their own file and run from init so main.go (the LSP entry point) stays
// untouched: `brain` with no arguments is still the language server.

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"brain/internal/bases"
	"brain/internal/daemon"
	"brain/internal/tex"
	"brain/internal/vault"
)

func init() {
	if len(os.Args) < 2 {
		return
	}
	var err error
	switch os.Args[1] {
	case "daemon":
		err = cmdDaemon(os.Args[2:])
	case "base":
		err = cmdBase(os.Args[2:])
	case "tex":
		err = cmdTex(os.Args[2:])
	case "socket":
		fs := flag.NewFlagSet("socket", flag.ExitOnError)
		root := fs.String("root", ".", "vault root")
		fs.Parse(os.Args[2:])
		abs, _ := filepath.Abs(*root)
		fmt.Println(daemon.SocketPath(abs))
	default:
		return
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "brain:", err)
		os.Exit(1)
	}
	os.Exit(0)
}

func cmdDaemon(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ExitOnError)
	root := fs.String("root", ".", "vault root")
	idle := fs.Duration("idle", 30*time.Minute, "exit after this long without clients (0 = never)")
	fs.Parse(args)
	abs, err := filepath.Abs(*root)
	if err != nil {
		return err
	}
	return daemon.Run(abs, *idle)
}

// cmdBase evaluates a .base file and prints the JSON result.
func cmdBase(args []string) error {
	fs := flag.NewFlagSet("base", flag.ExitOnError)
	root := fs.String("root", ".", "vault root")
	view := fs.String("view", "", "view name")
	this := fs.String("this", "", "file that `this` refers to")
	fs.Parse(args)
	if fs.NArg() != 1 {
		return fmt.Errorf("usage: brain base [-root dir] [-view name] file.base")
	}
	src, err := os.ReadFile(fs.Arg(0))
	if err != nil {
		return err
	}
	b, err := bases.ParseBase(string(src))
	if err != nil {
		return err
	}
	absRoot, _ := filepath.Abs(*root)
	v := vault.New(absRoot, "")
	v.Scan()
	thisPath := *this
	if thisPath == "" {
		thisPath = fs.Arg(0)
	}
	absThis, _ := filepath.Abs(thisPath)
	rel, _ := v.RelOf(absThis)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(b.Run(v, bases.Options{View: *view, This: rel}))
}

func cmdTex(args []string) error {
	fs := flag.NewFlagSet("tex", flag.ExitOnError)
	display := fs.Bool("display", true, "display style")
	fg := fs.String("fg", "", "foreground color")
	size := fs.Float64("size", 24, "font size in px")
	svg := fs.Bool("svg", false, "print SVG instead of writing a PNG")
	fs.Parse(args)
	src := fs.Arg(0)
	opt := tex.Options{Display: *display, FG: *fg, Size: *size}
	if *svg {
		s, err := tex.SVG(src, opt)
		if err != nil {
			return err
		}
		fmt.Println(s)
		return nil
	}
	res, err := daemon.RenderTeX(daemon.CacheDir(), src, opt)
	if err != nil {
		return err
	}
	fmt.Println(res["png"])
	return nil
}
