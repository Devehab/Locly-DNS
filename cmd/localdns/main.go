// Command localdns gives local services friendly hostnames by managing a
// dedicated section of the operating system's hosts file.
package main

import (
	"os"

	"github.com/devehab/locly-dns/internal/cli"
)

func main() {
	os.Exit(cli.Main())
}
