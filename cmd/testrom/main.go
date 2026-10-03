// Command testrom writes the homebrew test ROM, for trying Parlor without a
// real game: go run ./cmd/testrom /tmp/roms/parlor-test.gba
package main

import (
	"fmt"
	"os"

	"github.com/audemed44/parlor/internal/testrom"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: testrom OUT.gba")
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], testrom.ROM(), 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
