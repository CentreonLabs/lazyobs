// Command lazyobs is a terminal UI for the Centreon observability platform.
package main

import (
	"fmt"
	"os"

	"github.com/CentreonLabs/lazyobs/internal/version"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "version" {
		fmt.Println(version.String())
		return
	}
	fmt.Fprintln(os.Stderr, "lazyobs: the TUI is not implemented yet; try `lazyobs version`")
	os.Exit(1)
}
