package ui

import (
	"fmt"
	"strings"
	"time"

	"atlas.tail/internal/viewer"
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

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#AA0000")).
			Padding(0, 1).
			Bold(true)

	selectionStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#FFFFFF")).
			Foreground(lipgloss.Color("#000000"))

	newLinesStyle = lipgloss.NewStyle().
			Background(lipgloss.Color("#2E7D32")).
			Foreground(lipgloss.Color("#FFFFFF")).
			Bold(true).
			Padding(0, 1)
)

type tickMsg time.Time

func tickCmd(d time.Duration) tea.Cmd {
	return tea.Tick(d, func(t time.Time) tea.Msg { return tickMsg(t) })
}

type Model struct {
	processor *viewer.Processor
	viewport  viewport.Model
	ready     bool

	pollInterval time.Duration
	following    bool
	newCount     int // lines added since user paused

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

	errorMsg string
}

func NewModel(p *viewer.Processor, pollInterval time.Duration) Model {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.Prompt = " / "
	ti.Focus()

	return Model{
		processor:    p,
		searchInput:  ti,
		matchIndex:   -1,
		pollInterval: pollInterval,
		following:    true,
	}
}

func (m Model) Init() tea.Cmd {
	return tickCmd(m.pollInterval)
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
				return m, tickCmd(m.pollInterval)
			case "esc":
				m.searching = false
				m.searchInput.Reset()
				return m, tickCmd(m.pollInterval)
			}
		}
		m.searchInput, cmd = m.searchInput.Update(msg)
		return m, cmd
	}

	switch msg := msg.(type) {
	case tickMsg:
		added, err := m.processor.Poll()
		if err != nil {
			m.errorMsg = fmt.Sprintf("Tail error: %v", err)
		} else if added > 0 {
			m.errorMsg = ""
			if m.following {
				m.newCount = 0
				m.updateContent()
				m.scrollToBottom()
			} else {
				m.newCount += added
				m.updateContent()
			}
		}
		return m, tickCmd(m.pollInterval)

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
		case "w":
			m.processor.WrapLines = !m.processor.WrapLines
			m.updateContent()
			return m, nil
		case "f":
			m.following = !m.following
			if m.following {
				m.newCount = 0
				m.scrollToBottom()
				m.updateContent()
			}
			return m, nil
		case "G", "end":
			m.following = true
			m.newCount = 0
			m.cursorY = max(0, m.processor.LinesCount()-1)
			m.scrollToBottom()
			m.updateContent()
			return m, nil
		case "g", "home":
			m.following = false
			m.cursorY = 0
			m.viewport.SetYOffset(0)
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
		case "ctrl+r":
			if err := m.processor.Reload(); err != nil {
				m.errorMsg = fmt.Sprintf("Refresh failed: %v", err)
				return m, nil
			}
			m.errorMsg = ""
			m.newCount = 0
			if m.cursorY >= m.processor.LinesCount() {
				m.cursorY = max(0, m.processor.LinesCount()-1)
			}
			if m.searchQuery != "" {
				m.performSearch()
			}
			m.updateContent()
			if m.following {
				m.scrollToBottom()
			}
			return m, nil

		// Selection
		case "v":
			m.selecting = !m.selecting
			if m.selecting {
				m.selectStartY = m.cursorY
				m.selectStartX = m.cursorX
			}
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "up":
			m.following = false
			if m.cursorY > 0 {
				m.cursorY--
				m.ensureCursorVisible()
				lineLen := len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
				if m.cursorX > lineLen {
					m.cursorX = lineLen
				}
			}
			m.updateContent()
			return m, nil
		case "down":
			if m.cursorY < m.processor.LinesCount()-1 {
				m.cursorY++
				m.ensureCursorVisible()
				lineLen := len(strings.Split(m.processor.GetPlain(), "\n")[m.cursorY])
				if m.cursorX > lineLen {
					m.cursorX = lineLen
				}
			}
			m.updateContent()
			return m, nil
		case "pgup":
			m.following = false
			m.cursorY = max(0, m.cursorY-m.viewport.Height)
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "pgdown":
			m.cursorY = min(m.processor.LinesCount()-1, m.cursorY+m.viewport.Height)
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "ctrl+a":
			m.selecting = true
			m.selectStartY = 0
			m.selectStartX = 0
			m.cursorY = m.processor.LinesCount() - 1
			if m.cursorY < 0 {
				m.cursorY = 0
			}
			lines := strings.Split(m.processor.GetPlain(), "\n")
			m.cursorX = len(lines[m.cursorY])
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
			m.scrollToBottom()
			m.ready = true
		} else {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - verticalMarginHeight
			m.updateContent()
			if m.following {
				m.scrollToBottom()
			}
		}
	}

	m.viewport, cmd = m.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return m, tea.Batch(cmds...)
}

func (m *Model) scrollToBottom() {
	maxOffset := max(0, m.processor.LinesCount()-m.viewport.Height)
	m.viewport.SetYOffset(maxOffset)
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

		if start > len(line) {
			start = len(line)
		}
		if end > len(line) {
			end = len(line)
		}
		if start > end {
			start = end
		}

		selected = append(selected, line[start:end])
	}

	_ = clipboard.WriteAll(strings.Join(selected, "\n"))
}

func (m *Model) updateContent() {
	if m.selecting {
		m.processor.CursorY = -1
		m.processor.CursorX = -1
	} else {
		m.processor.CursorY = m.cursorY
		m.processor.CursorX = m.cursorX
	}
	content := m.processor.HighlightAll(m.searchQuery, m.matchIndex)
	m.viewport.SetContent(content)
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
	if len(m.matches) == 0 {
		return
	}
	m.matchIndex = (m.matchIndex + 1) % len(m.matches)
	m.jumpToMatch()
}

func (m *Model) findPrev() {
	if len(m.matches) == 0 {
		return
	}
	m.matchIndex = (m.matchIndex - 1 + len(m.matches)) % len(m.matches)
	m.jumpToMatch()
}

func (m *Model) jumpToMatch() {
	if m.matchIndex < 0 {
		return
	}
	offset := m.matches[m.matchIndex]
	plain := m.processor.GetPlain()

	lineNum := strings.Count(plain[:offset], "\n")
	lastNewline := strings.LastIndex(plain[:offset], "\n")
	colNum := offset
	if lastNewline != -1 {
		colNum = offset - lastNewline - 1
	}

	m.cursorY = lineNum
	m.cursorX = colNum
	m.following = false
	m.ensureCursorVisible()
}

func (m Model) View() string {
	if !m.ready {
		return "\n  Initializing..."
	}
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func (m Model) headerView() string {
	title := titleStyle.Render("ATLAS TAIL - " + m.processor.Path)
	live := viewer.LiveBadge(m.following)
	line := strings.Repeat("─", max(0, m.viewport.Width-lipgloss.Width(title)-lipgloss.Width(live)))
	return lipgloss.JoinHorizontal(lipgloss.Center, title, infoStyle.Render(line), live)
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
	if m.errorMsg != "" {
		status = errorStyle.Render(m.errorMsg) + " "
	} else if m.selecting {
		status = selectionStyle.Render(" SELECTING ") + " "
	} else if !m.following && m.newCount > 0 {
		status = newLinesStyle.Render(fmt.Sprintf("+%d new", m.newCount)) + " "
	}

	help := lipgloss.JoinHorizontal(lipgloss.Top,
		helpKeyStyle.Render(" q "), helpDescStyle.Render("quit "),
		helpKeyStyle.Render(" f "), helpDescStyle.Render("follow "),
		helpKeyStyle.Render(" G "), helpDescStyle.Render("bottom "),
		helpKeyStyle.Render(" g "), helpDescStyle.Render("top "),
		helpKeyStyle.Render(" l "), helpDescStyle.Render("lines "),
		helpKeyStyle.Render(" / "), helpDescStyle.Render("search "),
		helpKeyStyle.Render(" ^R "), helpDescStyle.Render("refresh "),
	)

	gap := max(0, m.viewport.Width-lipgloss.Width(help)-lipgloss.Width(percent)-lipgloss.Width(matchInfo)-lipgloss.Width(status)-2)
	line := strings.Repeat(" ", gap)

	return lipgloss.JoinHorizontal(lipgloss.Center, help, line, status, matchInfo, infoStyle.Render(percent))
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
