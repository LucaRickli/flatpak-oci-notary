// Command notary serves Flatpak OCI remote indexes for images on OCI
// registries. See root.go for the CLI (cobra + viper) and serve.go for the
// server command.
package main

import (
	"fmt"
	"os"
)

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
