# atlas.tail 📜

A beautiful, high-performance terminal log follower — `tail -f` with syntax highlighting and a TUI. Built with Go and the Atlas Suite philosophy.

## Overview

`atlas.tail` is a minimalist alternative to `tail -f`, designed to provide high-fidelity syntax highlighting and a smooth TUI experience for watching log files grow in real time.

## Features

- **Live Follow:** Polls the file for appended content and auto-scrolls to the latest line.
- **Truncation-safe:** Detects file truncation/rotation and reloads automatically.
- **Syntax Highlighting:** Powered by [Chroma](https://github.com/alecthomas/chroma) for 200+ languages.
- **Pause & Resume:** Pause the follow to inspect history; a badge shows how many new lines arrived while paused.
- **Searching:** Forward/backward incremental search inside the buffered tail.
- **Refresh:** `Ctrl+R` re-reads the file from scratch; shows an error if the file is missing.

## Installation

```bash
gobake build
```

## Usage

```bash
# Follow a log file interactively (default: last 10 lines, 300 ms polling)
atlas.tail server.log

# Start from the last 200 lines and poll every 100 ms
atlas.tail -N 200 -i 100 server.log

# Show the current tail and exit (no follow)
atlas.tail -F server.log

# Show version
atlas.tail -v
```

## Flags

- `-N <n>`       Number of lines to show initially (default `10`, `0` for all)
- `-i <ms>`      Poll interval in milliseconds (default `300`)
- `-l`           Show line numbers
- `-w`           Wrap long lines
- `-F`           Do not follow; print the tail and exit
- `-v`           Show version

## TUI Controls

- **f**          Toggle follow on/off
- **G, end**     Jump to bottom (resumes follow)
- **g, home**    Jump to top (pauses follow)
- **up / down**  Move cursor line (pauses follow)
- **pgup/pgdn**  Page up/down
- **/**          Search
- **n / N**      Next / previous match
- **v**          Start / stop selection
- **Ctrl+C**     Copy selection (or quit if no selection)
- **Ctrl+A**     Select all
- **Ctrl+R**     Refresh (reload file from disk); shows an error if the file is missing
- **l**          Toggle line numbers
- **w**          Toggle line wrapping
- **q, esc**     Quit
