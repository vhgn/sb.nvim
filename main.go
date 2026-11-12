// Command brain is a language server for a workspace of markdown notes.
// It speaks LSP over stdin/stdout.
package main

import (
	"errors"
	"fmt"
	"os"

	"brain/internal/lsp"
)

func main() {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("brain 0.1.0")
		return
	}
	err := lsp.NewServer(os.Stdin, os.Stdout).Run()
	switch {
	case err == nil, errors.Is(err, lsp.ErrExit):
		os.Exit(0)
	default:
		fmt.Fprintln(os.Stderr, "brain:", err)
		os.Exit(1)
	}
}
