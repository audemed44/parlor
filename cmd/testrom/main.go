// Command testrom writes a homebrew test ROM, for trying Parlor without a
// real game. The extension picks the console:
// go run ./cmd/testrom /tmp/roms/parlor-test.gba (or .gb, .gbc, .nes, .sfc, .nds)
package main

import (
	"fmt"
	"os"

	"github.com/audemed44/parlor/internal/library"
	"github.com/audemed44/parlor/internal/testrom"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: testrom OUT.gba|.gb|.gbc|.nes|.sfc|.nds")
		os.Exit(2)
	}
	rom := testrom.For(library.PlatformOf(os.Args[1]))
	if rom == nil {
		fmt.Fprintln(os.Stderr, "testrom: unknown console; use .gba, .gb, .gbc, .nes, .sfc or .nds")
		os.Exit(2)
	}
	if err := os.WriteFile(os.Args[1], rom, 0644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
