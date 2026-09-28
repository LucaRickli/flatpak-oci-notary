package main

import (
	"os"

	"github.com/lucarickli/flatpak-oci-notary/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
