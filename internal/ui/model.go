package ui

import (
	"fmt"
	"strings"

	"atlas.cat/internal/viewer"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#D4AF37")).
			Padding(0, 1)

	infoStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#555555")).
			Padding(0, 1)

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D4AF37")).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#AAAAAA"))

	matchCountStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#D4AF37")).
			Padding(0, 1).
			Bold(true)

	selectionStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#FFFFFF")).
			Foreground(lipgloss.Color("#000000"))

	cursorStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#D4AF37")).
			Foreground(lipgloss.Color("#000000")).
			Blink(true)
)

type Model struct {
	processor *viewer.Processor
	viewport  viewport.Model
	ready     bool

	// Search
	searching   bool
	searchInput textinput.Model
	searchQuery string
	matches     []int
	matchIndex  int

	// Selection and Cursor
	cursorY      int
	cursorX      int
	selecting    bool
	selectStartY int
	selectStartX int
}

func NewModel(p *viewer.Processor) Model {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.Prompt = " / "
	ti.Focus()

	return Model{
		processor:   p,
		searchInput: ti,
		matchIndex:  -1,
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		cmd  tea.Cmd
		cmds []tea.Cmd
	)

	if m.searching {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "enter":
				m.searchQuery = m.searchInput.Value()
				m.searching = false
				m.performSearch()
				m.updateContent()
				return m, nil
			case "esc":
				m.searching = false
				m.searchInput.Reset()
				return m, nil
			}
		}
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc":
			return m, tea.Quit
		case "ctrl+c":
			if m.selecting {
				m.copySelection()
				m.selecting = false
				return m, nil
			}
			return m, tea.Quit
		case "l":
			m.processor.ShowLineNumbers = !m.processor.ShowLineNumbers
			m.updateContent()
			return m, nil
		case "H":
			m.processor.HexMode = !m.processor.HexMode
			m.updateContent()
			return m, nil
		case "w":
			m.processor.WrapLines = !m.processor.WrapLines
			m.updateContent()
			return m, nil
		case "/":
			m.searching = true
			m.searchInput.Focus()
			return m, textinput.Blink
		case "n":
			m.findNext()
			m.updateContent()
			return m, nil
		case "N", "p":
			m.findPrev()
			m.updateContent()
			return m, nil

		// Extended Navigation
		case "ctrl+home":
			m.viewport.SetYOffset(0)
			m.cursorY = 0
		case "ctrl+end":
			m.viewport.SetYOffset(m.processor.LinesCount())
			m.cursorY = m.processor.LinesCount() - 1
		case "ctrl+up":
			m.viewport.LineUp(1)
		case "ctrl+down":
			m.viewport.LineDown(1)
		case "ctrl+pgup":
			m.viewport.ViewUp()
		case "ctrl+pgdown":
			m.viewport.ViewDown()
		case "shift+pgup", "pgup":
			if strings.HasPrefix(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			m.cursorY = max(0, m.cursorY-m.viewport.Height)
			// Clamp X
			lineLen := len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
			if m.cursorX > lineLen {
				m.cursorX = lineLen
			}
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "shift+pgdown", "pgdown":
			if strings.HasPrefix(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			m.cursorY = min(m.processor.LinesCount()-1, m.cursorY+m.viewport.Height)
			// Clamp X
			lineLen := len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
			if m.cursorX > lineLen {
				m.cursorX = lineLen
			}
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil

		// Selection
		case "v":
			if !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			} else {
				m.selecting = false
			}
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "shift+up", "up":
			if strings.HasPrefix(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			if m.cursorY > 0 {
				m.cursorY--
				m.ensureCursorVisible()
				// Clamp X
				lineLen := len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
				if m.cursorX > lineLen {
					m.cursorX = lineLen
				}
			}
			m.updateContent()
			return m, nil
		case "shift+down", "down":
			if strings.HasPrefix(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			if m.cursorY < m.processor.LinesCount()-1 {
				m.cursorY++
				m.ensureCursorVisible()
				// Clamp X
				lineLen := len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
				if m.cursorX > lineLen {
					m.cursorX = lineLen
				}
			}
			m.updateContent()
			return m, nil
		case "shift+left", "left":
			if strings.HasPrefix(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			if m.cursorX > 0 {
				m.cursorX--
			} else if m.cursorY > 0 {
				m.cursorY--
				m.cursorX = len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
				m.ensureCursorVisible()
			}
			m.updateContent()
			return m, nil
		case "shift+right", "right":
			if strings.HasPrefix(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			lineLen := len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
			if m.cursorX < lineLen {
				m.cursorX++
			} else if m.cursorY < m.processor.LinesCount()-1 {
				m.cursorY++
				m.cursorX = 0
				m.ensureCursorVisible()
			}
			m.updateContent()
			return m, nil
		case "ctrl+left", "ctrl+shift+left":
			if strings.Contains(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			m.moveWordLeft()
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "ctrl+right", "ctrl+shift+right":
			if strings.Contains(msg.String(), "shift") && !m.selecting {
				m.selecting = true
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			m.moveWordRight()
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "home":
			m.cursorX = 0
			m.updateContent()
			return m, nil
		case "end":
			m.cursorX = len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
			m.updateContent()
			return m, nil
		case "ctrl+a":
			m.selecting = true
			m.selectStartY = 0
			m.selectStartX = 0
			m.cursorY = m.processor.LinesCount() - 1
			m.cursorX = len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		}

	case tea.WindowSizeMsg:
		headerHeight := lipgloss.Height(m.headerView())
		footerHeight := lipgloss.Height(m.footerView())
		verticalMarginHeight := headerHeight + footerHeight

		m.processor.ViewportWidth = msg.Width
		if !m.ready {
			m.viewport = viewport.New(msg.Width, msg.Height-verticalMarginHeight)
			m.viewport.YPosition = headerHeight
			m.updateContent()
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
			m.updateContent()
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) copySelection() {
	if !m.selecting {
		return
	}

	sy, sx := m.selectStartY, m.selectStartX
	ey, ex := m.cursorY, m.cursorX

	if sy > ey || (sy == ey && sx > ex) {
		sy, sx, ey, ex = ey, ex, sy, sx
	}

	plainLines := strings.Split(m.processor.GetPlain(), "\n")
	var selected []string

	for y := sy; y <= ey && y < len(plainLines); y++ {
		line := plainLines[y]
		start, end := 0, len(line)

		if y == sy {
			start = sx
		}
		if y == ey {
			end = ex
		}

		if start > len(line) { start = len(line) }
		if end > len(line) { end = len(line) }
		if start > end { start = end }

		selected = append(selected, line[start:end])
	}

	_ = clipboard.WriteAll(strings.Join(selected, "\n"))
}

func (m *Model) updateContent() {
	var content string
	if m.selecting {
		content = m.renderSelection()
	} else {
		content = m.processor.HighlightAll(m.searchQuery, m.matchIndex)
	}
	m.viewport.SetContent(content)
}

func (m *Model) renderSelection() string {
	plain := m.processor.GetPlain()
	lines := strings.Split(plain, "\n")
	var finalLines []string

	sy, sx := m.selectStartY, m.selectStartX
	ey, ex := m.cursorY, m.cursorX

	if sy > ey || (sy == ey && sx > ex) {
		sy, sx, ey, ex = ey, ex, sy, sx
	}

	width := len(fmt.Sprintf("%d", len(lines)))

	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			break
		}

		styledLine := ""
		if i < sy || i > ey {
			// Outside selection
			styledLine = line
		} else if i > sy && i < ey {
			// Fully in selection
			styledLine = selectionStyle.Render(line)
			if line == "" { styledLine = selectionStyle.Render(" ") }
		} else if sy == ey {
			// Selection on a single line
			csx, cex := sx, ex
			if csx > len(line) { csx = len(line) }
			if cex > len(line) { cex = len(line) }
			if csx > cex { csx, cex = cex, csx }
			pre := line[:csx]
			sel := line[csx:cex]
			post := line[cex:]
			styledLine = pre + selectionStyle.Render(sel) + post
			if sel == "" && i == m.cursorY {
				// Show cursor if selection is empty but it's the cursor line
				// This part is handled by the "cursor" logic below if we want, 
				// but let's keep it simple.
			}
		} else if i == sy {
			// Start of multi-line selection
			csx := sx
			if csx > len(line) { csx = len(line) }
			pre := line[:csx]
			sel := line[csx:]
			styledLine = pre + selectionStyle.Render(sel)
		} else if i == ey {
			// End of multi-line selection
			cex := ex
			if cex > len(line) { cex = len(line) }
			sel := line[:cex]
			post := line[cex:]
			styledLine = selectionStyle.Render(sel) + post
		}

		// Apply cursor highlighting on top of selection or plain text
		if i == m.cursorY {
			// To keep it simple, if we are on the cursor line, we'll re-render it with cursor
			// But character-level cursor is better.
			// Let's just highlight the character at cursorX
			var curLine strings.Builder
			
			// We need to re-apply the selection logic per character to be perfect, 
			// but for a "cat" tool, line-level selection + character-level cursor is often enough.
			// User asked for "half of the words", so let's do character-level styling.
			
			// Re-calculating styledLine with character-level precision
			curLine.Reset()
			for x := 0; x <= len(line); x++ {
				char := " "
				if x < len(line) {
					char = string(line[x])
				}

				isSelected := false
				if sy == ey {
					isSelected = i == sy && x >= sx && x < ex
				} else if i == sy {
					isSelected = x >= sx
				} else if i == ey {
					isSelected = x < ex
				} else if i > sy && i < ey {
					isSelected = true
				}

				style := lipgloss.NewStyle()
				if x == m.cursorX {
					style = cursorStyle
				} else if isSelected {
					style = selectionStyle
				}

				if x < len(line) {
					curLine.WriteString(style.Render(char))
				} else if x == m.cursorX {
					curLine.WriteString(style.Render(" "))
				}
			}
			styledLine = curLine.String()
		}

		prefix := ""
		if m.processor.ShowLineNumbers {
			prefix = lipgloss.NewStyle().Foreground(lipgloss.Color("#555555")).MarginRight(1).Render(fmt.Sprintf("%*d", width, i+1))
		}

		finalLines = append(finalLines, prefix+styledLine)
	}

	return strings.Join(finalLines, "\n") + "\n" + lipgloss.NewStyle().Background(lipgloss.Color("#FF0000")).Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Padding(0, 1).Render("EOF")
}

func (m *Model) performSearch() {
	if m.searchQuery == "" {
		m.matches = nil
		m.matchIndex = -1
		return
	}

	m.matches = nil
	plain := m.processor.GetPlain()
	lowerPlain := strings.ToLower(plain)
	lowerQuery := strings.ToLower(m.searchQuery)

	start := 0
	for {
		idx := strings.Index(lowerPlain[start:], lowerQuery)
		if idx == -1 {
			break
		}
		m.matches = append(m.matches, start+idx)
		start += idx + len(lowerQuery)
	}

	if len(m.matches) > 0 {
		m.matchIndex = 0
		m.jumpToMatch()
	} else {
		m.matchIndex = -1
	}
}

func (m *Model) findNext() {
	if len(m.matches) == 0 { return }
	m.matchIndex = (m.matchIndex + 1) % len(m.matches)
	m.jumpToMatch()
}

func (m *Model) findPrev() {
	if len(m.matches) == 0 { return }
	m.matchIndex = (m.matchIndex - 1 + len(m.matches)) % len(m.matches)
	m.jumpToMatch()
}

func (m *Model) jumpToMatch() {
	if m.matchIndex < 0 { return }
	offset := m.matches[m.matchIndex]
	plain := m.processor.GetPlain()
	
	// Calculate line and column
	lineNum := strings.Count(plain[:offset], "\n")
	lastNewline := strings.LastIndex(plain[:offset], "\n")
	colNum := offset
	if lastNewline != -1 {
		colNum = offset - lastNewline - 1
	}

	m.cursorY = lineNum
	m.cursorX = colNum
	m.ensureCursorVisible()
}

func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func (m Model) headerView() string {
	title := titleStyle.Render("ATLAS CAT - " + m.processor.Path)
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, infoStyle.Render(line))
}

func (m Model) footerView() string {
	if m.searching {
		return m.searchInput.View()
	}

	percent := fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100)
	matchInfo := ""
	if len(m.matches) > 0 {
		matchInfo = matchCountStyle.Render(fmt.Sprintf("%d/%d", m.matchIndex+1, len(m.matches))) + " "
	}

	status := ""
	if m.selecting {
		status = selectionStyle.Render(" SELECTING ") + " "
	}

	help := lipgloss.JoinHorizontal(lipgloss.Top,
		helpKeyStyle.Render(" q "), helpDescStyle.Render("quit "),
		helpKeyStyle.Render(" v "), helpDescStyle.Render("select "),
		helpKeyStyle.Render(" ^C "), helpDescStyle.Render("copy "),
		helpKeyStyle.Render(" l "), helpDescStyle.Render("lines "),
		helpKeyStyle.Render(" / "), helpDescStyle.Render("search "),
	)

	gap := max(0, m.viewport.Width-lipgloss.Width(help)-lipgloss.Width(percent)-lipgloss.Width(matchInfo)-lipgloss.Width(status)-2)
	line := strings.Repeat(" ", gap)
	
	return lipgloss.JoinHorizontal(lipgloss.Center, help, line, status, matchInfo, infoStyle.Render(percent))
}

func max(a, b int) int {
	if a > b { return a }
	return b
}

func (m *Model) moveWordLeft() {
	lines := strings.Split(m.processor.GetPlain(), "\n")
	line := lines[m.cursorY]

	if m.cursorX == 0 {
		if m.cursorY > 0 {
			m.cursorY--
			m.cursorX = len(lines[m.cursorY])
		}
		return
	}

	// Move past whitespace
	i := m.cursorX - 1
	for i >= 0 && (line[i] == ' ' || line[i] == '\t') {
		i--
	}
	// Move past word
	for i >= 0 && (line[i] != ' ' && line[i] != '\t') {
		i--
	}
	m.cursorX = i + 1
}

func (m *Model) moveWordRight() {
	lines := strings.Split(m.processor.GetPlain(), "\n")
	line := lines[m.cursorY]

	if m.cursorX >= len(line) {
		if m.cursorY < len(lines)-1 {
			m.cursorY++
			m.cursorX = 0
		}
		return
	}

	// Move past word
	i := m.cursorX
	for i < len(line) && (line[i] != ' ' && line[i] != '\t') {
		i++
	}
	// Move past whitespace
	for i < len(line) && (line[i] == ' ' || line[i] == '\t') {
		i++
	}
	m.cursorX = i
}

func (m *Model) ensureCursorVisible() {
	half := m.viewport.Height / 2
	maxOffset := max(0, m.processor.LinesCount()-m.viewport.Height)

	offset := m.cursorY - half
	if offset < 0 {
		offset = 0
	} else if offset > maxOffset {
		offset = maxOffset
	}
	m.viewport.SetYOffset(offset)
}

func min(a, b int) int {
	if a < b { return a }
	return b
}
