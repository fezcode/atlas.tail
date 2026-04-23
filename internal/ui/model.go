package ui

import (
	"fmt"
	"path/filepath"
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
	brandStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#D4AF37")).
			Padding(0, 1)

	pathStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#E4E4E4")).
			Bold(true)

	statStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8A8A95"))

	statSepStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#3A3A44"))

	rateActiveStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2ECC71")).
			Bold(true)

	ruleStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#2A2A33"))

	helpKeyStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#D4AF37")).
			Bold(true)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#8A8A95"))

	matchCountStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#D4AF37")).
			Padding(0, 1).
			Bold(true)

	errorStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#FFFFFF")).
			Background(lipgloss.Color("#C0392B")).
			Padding(0, 1).
			Bold(true)

	selectionStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#000000")).
			Background(lipgloss.Color("#5DADE2")).
			Padding(0, 1).
			Bold(true)

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
	newCount     int

	searching   bool
	searchInput textinput.Model
	searchQuery string
	matches     []int
	matchIndex  int

	cursorY   int
	selecting bool
	selAnchor int

	errorMsg string
}

func NewModel(p *viewer.Processor, pollInterval time.Duration) Model {
	ti := textinput.New()
	ti.Placeholder = "filter..."
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
			m.errorMsg = fmt.Sprintf("tail: %v", err)
		} else {
			m.errorMsg = ""
			if added > 0 {
				if m.following {
					m.newCount = 0
					m.cursorY = max(0, m.processor.LinesCount()-1)
					m.updateContent()
					m.scrollToBottom()
				} else {
					m.newCount += added
					m.updateContent()
				}
			} else {
				m.updateContent()
			}
		}
		return m, tickCmd(m.pollInterval)

	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "esc":
			if m.selecting {
				m.selecting = false
				m.processor.SelStart = -1
				m.processor.SelEnd = -1
				m.updateContent()
				return m, nil
			}
			return m, tea.Quit
		case "ctrl+c":
			if m.selecting {
				m.copySelection()
				m.selecting = false
				m.processor.SelStart = -1
				m.processor.SelEnd = -1
				m.updateContent()
				return m, nil
			}
			return m, tea.Quit
		case "y":
			if m.selecting {
				m.copySelection()
				m.selecting = false
				m.processor.SelStart = -1
				m.processor.SelEnd = -1
				m.updateContent()
			}
			return m, nil
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
				m.cursorY = max(0, m.processor.LinesCount()-1)
				m.scrollToBottom()
				m.updateContent()
			}
			return m, nil
		case "G", "end":
			m.following = true
			m.newCount = 0
			m.cursorY = max(0, m.processor.LinesCount()-1)
			if m.selecting {
				m.processor.SelEnd = m.cursorY
			}
			m.scrollToBottom()
			m.updateContent()
			return m, nil
		case "g", "home":
			m.following = false
			m.cursorY = 0
			if m.selecting {
				m.processor.SelEnd = m.cursorY
			}
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
				m.errorMsg = fmt.Sprintf("refresh failed: %v", err)
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

		case "v":
			m.selecting = !m.selecting
			if m.selecting {
				m.selAnchor = m.cursorY
				m.processor.SelStart = m.cursorY
				m.processor.SelEnd = m.cursorY
			} else {
				m.processor.SelStart = -1
				m.processor.SelEnd = -1
			}
			m.updateContent()
			m.ensureCursorVisible()
			return m, nil
		case "up", "k":
			m.following = false
			if m.cursorY > 0 {
				m.cursorY--
			}
			if m.selecting {
				m.processor.SelEnd = m.cursorY
			}
			m.ensureCursorVisible()
			m.updateContent()
			return m, nil
		case "down", "j":
			if m.cursorY < m.processor.LinesCount()-1 {
				m.cursorY++
			}
			if m.selecting {
				m.processor.SelEnd = m.cursorY
			}
			m.ensureCursorVisible()
			m.updateContent()
			return m, nil
		case "pgup":
			m.following = false
			m.cursorY = max(0, m.cursorY-m.viewport.Height)
			if m.selecting {
				m.processor.SelEnd = m.cursorY
			}
			m.ensureCursorVisible()
			m.updateContent()
			return m, nil
		case "pgdown":
			m.cursorY = min(m.processor.LinesCount()-1, m.cursorY+m.viewport.Height)
			if m.selecting {
				m.processor.SelEnd = m.cursorY
			}
			m.ensureCursorVisible()
			m.updateContent()
			return m, nil
		case "ctrl+a":
			m.selecting = true
			m.selAnchor = 0
			m.processor.SelStart = 0
			m.cursorY = max(0, m.processor.LinesCount()-1)
			m.processor.SelEnd = m.cursorY
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
	if m.processor.SelStart < 0 || m.processor.SelEnd < 0 {
		return
	}
	lo, hi := m.processor.SelStart, m.processor.SelEnd
	if lo > hi {
		lo, hi = hi, lo
	}
	var out []string
	for i := lo; i <= hi && i < m.processor.LinesCount(); i++ {
		out = append(out, m.processor.Line(i))
	}
	_ = clipboard.WriteAll(strings.Join(out, "\n"))
}

func (m *Model) updateContent() {
	if m.selecting {
		m.processor.CursorY = -1
	} else {
		m.processor.CursorY = m.cursorY
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
	lowerQuery := strings.ToLower(m.searchQuery)
	for i := 0; i < m.processor.LinesCount(); i++ {
		if strings.Contains(strings.ToLower(m.processor.Line(i)), lowerQuery) {
			m.matches = append(m.matches, i)
		}
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
	if m.matchIndex < 0 || m.matchIndex >= len(m.matches) {
		return
	}
	m.cursorY = m.matches[m.matchIndex]
	m.following = false
	m.ensureCursorVisible()
}

func (m Model) View() string {
	if !m.ready {
		return "\n  initializing..."
	}
	return fmt.Sprintf("%s\n%s\n%s", m.headerView(), m.viewport.View(), m.footerView())
}

func (m Model) headerView() string {
	brand := brandStyle.Render("▌ atlas.tail")
	path := pathStyle.Render(filepath.Base(m.processor.Path))

	sep := statSepStyle.Render(" · ")
	count := statStyle.Render(fmt.Sprintf("%d lines", m.processor.LinesCount()))
	size := statStyle.Render(viewer.FormatBytes(m.processor.FileSize()))

	rate := m.processor.Rate()
	rateStr := statStyle.Render("0.0 l/s")
	if rate >= 0.1 {
		rateStr = rateActiveStyle.Render(fmt.Sprintf("%.1f l/s", rate))
	}

	live := viewer.LiveBadge(m.following)

	left := lipgloss.JoinHorizontal(lipgloss.Top,
		brand, " ", path, sep, count, sep, size, sep, rateStr,
	)

	gap := max(0, m.viewport.Width-lipgloss.Width(left)-lipgloss.Width(live))
	rule := ruleStyle.Render(strings.Repeat(" ", gap))

	return lipgloss.JoinHorizontal(lipgloss.Top, left, rule, live)
}

func (m Model) footerView() string {
	if m.searching {
		return m.searchInput.View()
	}

	percent := statStyle.Render(fmt.Sprintf("%3.f%%", m.viewport.ScrollPercent()*100))

	matchInfo := ""
	if len(m.matches) > 0 {
		matchInfo = matchCountStyle.Render(fmt.Sprintf("%d/%d", m.matchIndex+1, len(m.matches))) + " "
	}

	status := ""
	switch {
	case m.errorMsg != "":
		status = errorStyle.Render(m.errorMsg) + " "
	case m.selecting:
		n := 0
		if m.processor.SelStart >= 0 {
			lo, hi := m.processor.SelStart, m.processor.SelEnd
			if lo > hi {
				lo, hi = hi, lo
			}
			n = hi - lo + 1
		}
		status = selectionStyle.Render(fmt.Sprintf("SELECT %d", n)) + " "
	case !m.following && m.newCount > 0:
		status = newLinesStyle.Render(fmt.Sprintf("+%d", m.newCount)) + " "
	}

	help := lipgloss.JoinHorizontal(lipgloss.Top,
		helpKeyStyle.Render(" f"), helpDescStyle.Render(" follow "),
		helpKeyStyle.Render(" G"), helpDescStyle.Render(" tail "),
		helpKeyStyle.Render(" g"), helpDescStyle.Render(" top "),
		helpKeyStyle.Render(" /"), helpDescStyle.Render(" find "),
		helpKeyStyle.Render(" v"), helpDescStyle.Render(" select "),
		helpKeyStyle.Render(" y"), helpDescStyle.Render(" yank "),
		helpKeyStyle.Render(" l"), helpDescStyle.Render(" # "),
		helpKeyStyle.Render(" q"), helpDescStyle.Render(" quit "),
	)

	gap := max(0, m.viewport.Width-lipgloss.Width(help)-lipgloss.Width(percent)-lipgloss.Width(matchInfo)-lipgloss.Width(status)-1)
	spacer := strings.Repeat(" ", gap)

	return lipgloss.JoinHorizontal(lipgloss.Top, help, spacer, status, matchInfo, percent)
}

func (m *Model) ensureCursorVisible() {
	if m.viewport.Height <= 0 {
		return
	}
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
