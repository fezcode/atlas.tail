package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"atlas.tail/internal/ui"
	"atlas.tail/internal/viewer"
	tea "github.com/charmbracelet/bubbletea"
)

var Version = "dev"

func main() {
	showVersion := flag.Bool("v", false, "Show version")
	showVersionLong := flag.Bool("version", false, "Show version")
	showLineNumbers := flag.Bool("l", false, "Show line numbers")
	wrapLines := flag.Bool("w", false, "Wrap long lines")
	initialLines := flag.Int("N", 10, "Number of lines to show initially (0 for all)")
	pollMs := flag.Int("i", 300, "Poll interval in milliseconds")
	noFollow := flag.Bool("F", false, "Do not follow; just show the tail and exit to stdout")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Atlas Tail - A beautiful terminal log follower.\n\n")
		fmt.Fprintf(os.Stderr, "Usage:\n  atlas.tail [flags] <file>\n\n")
		fmt.Fprintf(os.Stderr, "Flags:\n")
		flag.PrintDefaults()
	}

	flag.Parse()

	if *showVersion || *showVersionLong {
		fmt.Printf("atlas.tail v%s\n", Version)
		return
	}

	args := flag.Args()
	if len(args) < 1 {
		flag.Usage()
		os.Exit(1)
	}

	filePath := args[0]
	p, err := viewer.NewProcessor(filePath, *showLineNumbers, *wrapLines, *initialLines)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error initializing: %v\n", err)
		os.Exit(1)
	}

	if *noFollow {
		fmt.Print(p.HighlightAll("", -1))
		return
	}

	pModel := ui.NewModel(p, time.Duration(*pollMs)*time.Millisecond)
	prog := tea.NewProgram(pModel, tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := prog.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}
