package viewer

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

type Level int

const (
	LevelNone Level = iota
	LevelTrace
	LevelDebug
	LevelInfo
	LevelWarn
	LevelError
)

var (
	tsStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C6C78"))
	defaultFG = lipgloss.NewStyle().Foreground(lipgloss.Color("#D4D4DC"))
	errorFG   = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF6B6B"))
	warnFG    = lipgloss.NewStyle().Foreground(lipgloss.Color("#F1C40F"))
	infoFG    = lipgloss.NewStyle().Foreground(lipgloss.Color("#5DADE2"))
	debugFG   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9AA0A6"))
	traceFG   = lipgloss.NewStyle().Foreground(lipgloss.Color("#6C6C78"))

	lineNumStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3A3A44")).MarginRight(1)

	gutterCursor    = lipgloss.NewStyle().Foreground(lipgloss.Color("#D4AF37")).Bold(true)
	gutterSelection = lipgloss.NewStyle().Foreground(lipgloss.Color("#5DADE2")).Bold(true)

	searchMatchStyle  = lipgloss.NewStyle().Background(lipgloss.Color("#D4AF37")).Foreground(lipgloss.Color("#000000"))
	currentMatchStyle = lipgloss.NewStyle().Background(lipgloss.Color("#FFFFFF")).Foreground(lipgloss.Color("#000000")).Bold(true)

	liveStyle   = lipgloss.NewStyle().Background(lipgloss.Color("#2E7D32")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1)
	pausedStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888")).Padding(0, 1)
)

var (
	reTimestamp = regexp.MustCompile(`^(?:\[?\d{4}[-/]\d{2}[-/]\d{2}[T ]\d{2}:\d{2}:\d{2}(?:[.,]\d+)?(?:Z|[+-]\d{2}:?\d{2})?\]?|\[?\d{2}:\d{2}:\d{2}(?:[.,]\d+)?\]?)\s*`)
	reError     = regexp.MustCompile(`(?i)\b(error|err|fatal|panic|critical|crit|severe|fail(ed|ure)?)\b`)
	reWarn      = regexp.MustCompile(`(?i)\b(warn|warning)\b`)
	reInfo      = regexp.MustCompile(`(?i)\b(info|notice)\b`)
	reDebug     = regexp.MustCompile(`(?i)\b(debug|dbg)\b`)
	reTrace     = regexp.MustCompile(`(?i)\b(trace|verbose|vrb)\b`)
)

type rateSample struct {
	t     time.Time
	added int
}

type Processor struct {
	Path            string
	ShowLineNumbers bool
	WrapLines       bool
	ViewportWidth   int

	// CursorY < 0 disables cursor gutter caret.
	CursorY int

	// SelStart/SelEnd are inclusive line indices. If either < 0, no selection.
	SelStart int
	SelEnd   int

	lines    []string
	offset   int64
	pending  string
	initialN int

	history []rateSample
}

func NewProcessor(path string, showLines, wrap bool, initialLines int) (*Processor, error) {
	p := &Processor{
		Path:            path,
		ShowLineNumbers: showLines,
		WrapLines:       wrap,
		initialN:        initialLines,
		CursorY:         -1,
		SelStart:        -1,
		SelEnd:          -1,
	}
	if err := p.Reload(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *Processor) Reload() error {
	f, err := os.Open(p.Path)
	if err != nil {
		return err
	}
	defer f.Close()

	var all []string
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
	for scanner.Scan() {
		all = append(all, scanner.Text())
	}
	if err := scanner.Err(); err != nil {
		return err
	}

	if p.initialN > 0 && len(all) > p.initialN {
		all = all[len(all)-p.initialN:]
	}

	info, err := os.Stat(p.Path)
	if err != nil {
		return err
	}
	p.lines = all
	p.offset = info.Size()
	p.pending = ""
	return nil
}

func (p *Processor) Poll() (int, error) {
	info, err := os.Stat(p.Path)
	if err != nil {
		p.recordRate(0)
		return 0, err
	}

	size := info.Size()
	if size < p.offset {
		before := len(p.lines)
		if err := p.Reload(); err != nil {
			return 0, err
		}
		added := len(p.lines) - before
		p.recordRate(added)
		return added, nil
	}
	if size == p.offset {
		p.recordRate(0)
		return 0, nil
	}

	f, err := os.Open(p.Path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	if _, err := f.Seek(p.offset, io.SeekStart); err != nil {
		return 0, err
	}

	buf := make([]byte, size-p.offset)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.ErrUnexpectedEOF {
		return 0, err
	}
	p.offset += int64(n)

	data := p.pending + string(buf[:n])
	p.pending = ""

	added := 0
	for {
		idx := strings.IndexByte(data, '\n')
		if idx < 0 {
			p.pending = data
			break
		}
		line := strings.TrimRight(data[:idx], "\r")
		p.lines = append(p.lines, line)
		added++
		data = data[idx+1:]
	}
	p.recordRate(added)
	return added, nil
}

func (p *Processor) recordRate(added int) {
	now := time.Now()
	p.history = append(p.history, rateSample{now, added})
	cutoff := now.Add(-5 * time.Second)
	i := 0
	for i < len(p.history) && p.history[i].t.Before(cutoff) {
		i++
	}
	if i > 0 {
		p.history = p.history[i:]
	}
}

// Rate returns lines-per-second over the last ~5 seconds.
func (p *Processor) Rate() float64 {
	if len(p.history) < 2 {
		return 0
	}
	total := 0
	for _, s := range p.history {
		total += s.added
	}
	dur := time.Since(p.history[0].t).Seconds()
	if dur < 0.25 {
		return 0
	}
	return float64(total) / dur
}

func (p *Processor) FileSize() int64 {
	info, err := os.Stat(p.Path)
	if err != nil {
		return 0
	}
	return info.Size()
}

func (p *Processor) GetPlain() string {
	return strings.Join(p.lines, "\n")
}

func (p *Processor) Line(i int) string {
	if i < 0 || i >= len(p.lines) {
		return ""
	}
	return p.lines[i]
}

func (p *Processor) LinesCount() int {
	return len(p.lines)
}

func (p *Processor) inSelection(i int) bool {
	if p.SelStart < 0 || p.SelEnd < 0 {
		return false
	}
	lo, hi := p.SelStart, p.SelEnd
	if lo > hi {
		lo, hi = hi, lo
	}
	return i >= lo && i <= hi
}

func detectLevel(rest string) Level {
	// Scan only the prefix (first ~40 chars) for a level keyword so ERROR
	// mentioned deep inside a payload doesn't repaint the whole line.
	prefix := rest
	if len(prefix) > 40 {
		prefix = prefix[:40]
	}
	switch {
	case reError.MatchString(prefix):
		return LevelError
	case reWarn.MatchString(prefix):
		return LevelWarn
	case reInfo.MatchString(prefix):
		return LevelInfo
	case reDebug.MatchString(prefix):
		return LevelDebug
	case reTrace.MatchString(prefix):
		return LevelTrace
	}
	return LevelNone
}

func levelFG(l Level) lipgloss.Style {
	switch l {
	case LevelError:
		return errorFG
	case LevelWarn:
		return warnFG
	case LevelInfo:
		return infoFG
	case LevelDebug:
		return debugFG
	case LevelTrace:
		return traceFG
	}
	return defaultFG
}

// HighlightAll renders every buffered line with gutter, optional line numbers,
// timestamp dimming, level-based coloring, and search match highlighting.
func (p *Processor) HighlightAll(searchQuery string, matchIndex int) string {
	numWidth := len(fmt.Sprintf("%d", max(1, len(p.lines))))
	var out strings.Builder
	matchCounter := 0

	for i, line := range p.lines {
		gutter := "  "
		switch {
		case i == p.CursorY:
			gutter = gutterCursor.Render("▶") + " "
		case p.inSelection(i):
			gutter = gutterSelection.Render("▌") + " "
		}

		lineNumPart := ""
		indentWidth := 2
		if p.ShowLineNumbers {
			lineNumPart = lineNumStyle.Render(fmt.Sprintf("%*d", numWidth, i+1))
			indentWidth += numWidth + 1
		}

		body, newCount := p.renderBody(line, searchQuery, matchIndex, matchCounter)
		matchCounter = newCount

		rendered := gutter + lineNumPart + body

		if p.WrapLines && p.ViewportWidth > indentWidth+8 {
			contentWidth := p.ViewportWidth - indentWidth
			wrapped := lipgloss.NewStyle().Width(contentWidth).Render(body)
			subLines := strings.Split(wrapped, "\n")
			for j, sl := range subLines {
				if j == 0 {
					out.WriteString(gutter + lineNumPart + sl)
				} else {
					out.WriteString(strings.Repeat(" ", indentWidth) + sl)
				}
				out.WriteByte('\n')
			}
			continue
		}

		out.WriteString(rendered)
		out.WriteByte('\n')
	}

	return out.String()
}

func (p *Processor) renderBody(line, query string, targetMatch, counter int) (string, int) {
	tsPart := ""
	rest := line
	if m := reTimestamp.FindString(line); m != "" {
		tsPart = tsStyle.Render(m)
		rest = line[len(m):]
	}

	fg := levelFG(detectLevel(rest))

	if query == "" {
		return tsPart + fg.Render(rest), counter
	}

	var out strings.Builder
	out.WriteString(tsPart)

	lowerRest := strings.ToLower(rest)
	lowerQuery := strings.ToLower(query)
	cursor := 0
	for {
		idx := strings.Index(lowerRest[cursor:], lowerQuery)
		if idx == -1 {
			out.WriteString(fg.Render(rest[cursor:]))
			break
		}
		idx += cursor
		if idx > cursor {
			out.WriteString(fg.Render(rest[cursor:idx]))
		}
		matchText := rest[idx : idx+len(query)]
		style := searchMatchStyle
		if counter == targetMatch {
			style = currentMatchStyle
		}
		out.WriteString(style.Render(matchText))
		counter++
		cursor = idx + len(query)
	}

	return out.String(), counter
}

// LiveBadge returns the LIVE / paused status indicator used by the UI header.
func LiveBadge(following bool) string {
	if following {
		return liveStyle.Render("● LIVE")
	}
	return pausedStyle.Render("○ PAUSED")
}

// FormatBytes renders a byte count in a short human-friendly form.
func FormatBytes(n int64) string {
	const (
		kb = 1024
		mb = kb * 1024
		gb = mb * 1024
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.1f KB", float64(n)/float64(kb))
	}
	return fmt.Sprintf("%d B", n)
}
