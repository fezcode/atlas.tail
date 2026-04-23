package viewer

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/alecthomas/chroma/v2"
	"github.com/alecthomas/chroma/v2/formatters"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/charmbracelet/lipgloss"
)

var (
	eofStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#FF0000")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)

	liveStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2E7D32")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)

	pausedStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888")).
			Padding(0, 1)

	lineNumStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			MarginRight(1)

	searchMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#D4AF37")).
				Foreground(lipgloss.Color("#000000"))

	currentMatchStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#FFFFFF")).
				Foreground(lipgloss.Color("#000000")).
				Bold(true)

	cursorOverlayStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#D4AF37")).
				Foreground(lipgloss.Color("#000000"))
)

type Processor struct {
	Path            string
	ShowLineNumbers bool
	WrapLines       bool
	ViewportWidth   int

	// CursorY/CursorX < 0 disable cursor overlay.
	CursorY int
	CursorX int

	lines    []string
	offset   int64  // byte offset we have already read up to
	pending  string // partial line carried over between reads
	initialN int
}

// NewProcessor opens the file and loads the last initialLines lines (0 = all).
func NewProcessor(path string, showLines, wrap bool, initialLines int) (*Processor, error) {
	p := &Processor{
		Path:            path,
		ShowLineNumbers: showLines,
		WrapLines:       wrap,
		initialN:        initialLines,
		CursorY:         -1,
		CursorX:         -1,
	}

	if err := p.Reload(); err != nil {
		return nil, err
	}
	return p, nil
}

// Reload fully re-reads the file from the beginning, keeping the last
// initialN lines if set. Also used when the file is truncated or rotated.
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

// Poll reads any new bytes appended since the last read. Returns the number of
// new complete lines appended (for notification purposes) and any error.
// If the file shrinks, it is treated as a truncation/rotation and fully reloaded.
func (p *Processor) Poll() (int, error) {
	info, err := os.Stat(p.Path)
	if err != nil {
		return 0, err
	}

	size := info.Size()
	if size < p.offset {
		before := len(p.lines)
		if err := p.Reload(); err != nil {
			return 0, err
		}
		return len(p.lines) - before, nil
	}
	if size == p.offset {
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
	return added, nil
}

func (p *Processor) GetPlain() string {
	return strings.Join(p.lines, "\n")
}

func (p *Processor) LinesCount() int {
	return len(p.lines)
}

func (p *Processor) HighlightAll(searchQuery string, matchIndex int) string {
	content := p.GetPlain()

	lexer := lexers.Get(p.Path)
	if lexer == nil {
		lexer = lexers.Analyse(content)
	}
	if lexer == nil {
		lexer = lexers.Fallback
	}
	lexer = chroma.Coalesce(lexer)

	style := styles.Get("monokai")
	formatter := formatters.Get("terminal256")

	iterator, _ := lexer.Tokenise(nil, content)
	var buf bytes.Buffer
	formatter.Format(&buf, style, iterator)

	highlighted := buf.String()

	if searchQuery != "" {
		highlighted = p.applySearchHighlight(highlighted, searchQuery, matchIndex)
	}

	lines := strings.Split(highlighted, "\n")
	var finalLines []string
	width := len(fmt.Sprintf("%d", len(lines)))

	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}
		if i == p.CursorY && p.CursorX >= 0 {
			line = overlayCursor(line, p.CursorX)
		}
		prefix := ""
		if p.ShowLineNumbers {
			prefix = lineNumStyle.Render(fmt.Sprintf("%*d", width, i+1))
		}

		if p.WrapLines && p.ViewportWidth > 0 {
			contentWidth := p.ViewportWidth
			if p.ShowLineNumbers {
				contentWidth -= (width + 1)
			}
			if contentWidth > 0 {
				wrapped := lipgloss.NewStyle().Width(contentWidth).Render(line)
				subLines := strings.Split(wrapped, "\n")
				for j, sl := range subLines {
					if j == 0 {
						finalLines = append(finalLines, prefix+sl)
					} else {
						finalLines = append(finalLines, strings.Repeat(" ", width+1)+sl)
					}
				}
				continue
			}
		}
		finalLines = append(finalLines, prefix+line)
	}

	highlighted = strings.Join(finalLines, "\n")
	if !strings.HasSuffix(highlighted, "\n") {
		highlighted += "\n"
	}
	highlighted += eofStyle.Render("EOF")
	return highlighted
}

// overlayCursor paints a single-cell cursor on an ANSI-styled line at the
// given plain-text column, skipping over SGR escape sequences.
func overlayCursor(line string, col int) string {
	var out strings.Builder
	plainCol := 0
	i := 0
	placed := false

	for i < len(line) {
		if i+1 < len(line) && line[i] == '\x1b' && line[i+1] == '[' {
			end := strings.IndexAny(line[i+2:], "mABCDHJKfhnpsu")
			if end == -1 {
				out.WriteString(line[i:])
				i = len(line)
				continue
			}
			end += i + 2 + 1
			out.WriteString(line[i:end])
			i = end
			continue
		}

		if plainCol == col && !placed {
			ch := string(line[i])
			out.WriteString("\x1b[0m")
			out.WriteString(cursorOverlayStyle.Render(ch))
			placed = true
			i++
			plainCol++
			continue
		}

		out.WriteByte(line[i])
		i++
		plainCol++
	}

	if !placed && plainCol == col {
		out.WriteString("\x1b[0m")
		out.WriteString(cursorOverlayStyle.Render(" "))
	}

	return out.String()
}

func (p *Processor) applySearchHighlight(highlighted, query string, matchIndex int) string {
	var result strings.Builder
	cursor := 0
	matchCounter := 0

	for {
		start := strings.Index(highlighted[cursor:], "\x1b[")
		if start == -1 {
			res, count := p.highlightPlainPart(highlighted[cursor:], query, matchIndex, matchCounter)
			result.WriteString(res)
			matchCounter = count
			break
		}

		start += cursor
		res, count := p.highlightPlainPart(highlighted[cursor:start], query, matchIndex, matchCounter)
		result.WriteString(res)
		matchCounter = count

		end := strings.IndexAny(highlighted[start:], "mABCDHJKfhnpsu")
		if end == -1 {
			result.WriteString(highlighted[start:])
			break
		}
		end += start + 1
		result.WriteString(highlighted[start:end])
		cursor = end
	}

	return result.String()
}

func (p *Processor) highlightPlainPart(text, query string, targetIdx, currentCount int) (string, int) {
	if query == "" || text == "" {
		return text, currentCount
	}

	lowerText := strings.ToLower(text)
	lowerQuery := strings.ToLower(query)

	var result strings.Builder
	cursor := 0
	count := currentCount
	for {
		idx := strings.Index(lowerText[cursor:], lowerQuery)
		if idx == -1 {
			result.WriteString(text[cursor:])
			break
		}

		idx += cursor
		result.WriteString(text[cursor:idx])

		matchText := text[idx : idx+len(query)]
		style := searchMatchStyle
		if count == targetIdx {
			style = currentMatchStyle
		}
		result.WriteString(style.Render(matchText))

		count++
		cursor = idx + len(query)
	}
	return result.String(), count
}

// LiveBadge returns the LIVE / paused status indicator used by the UI footer.
func LiveBadge(following bool) string {
	if following {
		return liveStyle.Render("● LIVE")
	}
	return pausedStyle.Render("○ paused")
}
