// Package tui provides the interactive Bubble Tea select-and-delete front-end.
// It is a thin presenter over the wolf core and contains no deletion or sizing
// logic of its own.
package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"it.kluth.buildcleaner/internal/wolf"
)

// Options configures an interactive run.
type Options struct {
	Root      string
	Global    bool
	Permanent bool // delete permanently instead of moving to the trash
}

type state int

const (
	stateScanning state = iota
	stateSelecting
	stateConfirm
	stateDeleting
	stateDone
)

type item struct {
	target   wolf.Target
	size     int64
	sized    bool
	selected bool
}

type scanMsg struct{ targets []wolf.Target }
type sizeMsg struct {
	path string
	size int64
}
type sizingDoneMsg struct{}
type delMsg struct {
	path string
	size int64
	err  error
}

// Model is the Bubble Tea model for the select-and-delete TUI.
type Model struct {
	opts Options

	state   state
	spinner spinner.Model
	filter  textinput.Model
	prog    progress.Model

	items    []*item
	byPath   map[string]*item
	view     []int
	allSized bool

	filtering  bool
	onlyGlobal bool

	cursor int
	offset int
	height int
	width  int

	// Injected for testability; defaulted to wolf in New.
	findFn   func() []wolf.Target
	sizeFn   func(string) int64
	deleteFn func(wolf.Target) error

	sizeCh   chan sizeMsg
	delQueue []int
	delIndex int
	freed    int64
	failures []wolf.Failure
}

// New constructs a Model wired to the real wolf operations.
func New(opts Options) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	ti := textinput.New()
	ti.Placeholder = "filter by path or type"
	m := Model{
		opts:    opts,
		spinner: sp,
		filter:  ti,
		prog:    progress.New(progress.WithDefaultGradient()),
		height:  20,
		width:   80,
		findFn:  func() []wolf.Target { return wolf.Find(wolf.Options{Root: opts.Root, IncludeGlobal: opts.Global}) },
		sizeFn:  wolf.Measure,
		deleteFn: func(t wolf.Target) error {
			how := wolf.ToTrash
			if opts.Permanent {
				how = wolf.Permanent
			}
			_, failed := wolf.Delete([]wolf.Target{t}, how)
			if len(failed) > 0 {
				return failed[0].Err
			}
			return nil
		},
	}
	m.resetForScan() // initialize per-scan state (state, byPath, ...) once
	return m
}

// resetForScan clears the per-scan state so a fresh scan can repopulate the
// model, returning it to stateScanning. View preferences (the filter text and
// the onlyGlobal toggle) and the injected functions/opts are preserved.
func (m *Model) resetForScan() {
	m.items = nil
	m.byPath = map[string]*item{}
	m.view = nil
	m.allSized = false
	m.cursor = 0
	m.offset = 0
	m.delQueue = nil
	m.delIndex = 0
	m.freed = 0
	m.failures = nil
	m.sizeCh = nil
	m.filtering = false
	m.filter.Blur()
	m.state = stateScanning
}

// rescan discards the current results and kicks off a fresh scan, returning to
// the scanning -> selecting flow (preserving the filter and globals toggle).
func (m Model) rescan() (tea.Model, tea.Cmd) {
	m.resetForScan()
	return m, tea.Batch(m.spinner.Tick, findCmd(m.findFn))
}

// Init starts the spinner and the background scan.
func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, findCmd(m.findFn))
}

func findCmd(fn func() []wolf.Target) tea.Cmd {
	return func() tea.Msg { return scanMsg{targets: fn()} }
}

// startSizing launches a producer goroutine sizing each item in order, pushing
// results onto a channel; returns the command that reads the first result.
func (m *Model) startSizing() tea.Cmd {
	m.sizeCh = make(chan sizeMsg, 64)
	items := m.items
	sizeFn := m.sizeFn
	ch := m.sizeCh
	go func() {
		for _, it := range items {
			ch <- sizeMsg{path: it.target.Path, size: sizeFn(it.target.Path)}
		}
		close(ch)
	}()
	return listenSize(ch)
}

func listenSize(ch chan sizeMsg) tea.Cmd {
	return func() tea.Msg {
		msg, ok := <-ch
		if !ok {
			return sizingDoneMsg{}
		}
		return msg
	}
}

// Update is the Bubble Tea state-transition function.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = max(3, msg.Height-6)
		m.prog.Width = max(10, msg.Width-20)
		m.clampOffset()
		return m, nil
	case spinner.TickMsg:
		if m.state == stateScanning {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			return m, cmd
		}
		return m, nil
	case scanMsg:
		m.items = make([]*item, 0, len(msg.targets))
		for _, t := range msg.targets {
			it := &item{target: t, selected: false}
			m.items = append(m.items, it)
			m.byPath[t.Path] = it
		}
		m.state = stateSelecting
		m.rebuildView()
		return m, m.startSizing()
	case sizeMsg:
		if it, ok := m.byPath[msg.path]; ok {
			it.size = msg.size
			it.sized = true
		}
		return m, listenSize(m.sizeCh)
	case sizingDoneMsg:
		m.allSized = true
		m.rebuildView()
		return m, nil
	case delMsg:
		if msg.err != nil {
			m.failures = append(m.failures, wolf.Failure{Path: msg.path, Err: msg.err})
		} else {
			m.freed += msg.size
		}
		m.delIndex++
		if m.delIndex < len(m.delQueue) {
			return m, m.deleteNext()
		}
		m.state = stateDone
		return m, nil
	case tea.KeyMsg:
		return m.handleKey(msg)
	}
	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.state {
	case stateSelecting:
		return m.handleSelectingKey(msg)
	case stateConfirm:
		switch msg.String() {
		case "y":
			return m.beginDelete()
		case "n", "esc", "q":
			m.state = stateSelecting
			return m, nil
		}
		return m, nil
	case stateDone:
		switch msg.String() {
		case "r", "enter":
			return m.rescan()
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		}
		return m, nil
	}
	return m, nil
}

func (m Model) handleSelectingKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.filtering {
		switch msg.Type {
		case tea.KeyEnter:
			m.filtering = false
			m.filter.Blur()
			return m, nil
		case tea.KeyEsc:
			m.filtering = false
			m.filter.SetValue("")
			m.filter.Blur()
			m.rebuildView()
			return m, nil
		}
		var cmd tea.Cmd
		m.filter, cmd = m.filter.Update(msg)
		m.rebuildView()
		return m, cmd
	}

	switch msg.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "up", "k":
		m.moveCursor(-1)
	case "down", "j":
		m.moveCursor(1)
	case "pgup":
		m.moveCursor(-m.height)
	case "pgdown":
		m.moveCursor(m.height)
	case " ":
		if len(m.view) > 0 {
			it := m.items[m.view[m.cursor]]
			it.selected = !it.selected
		}
	case "a":
		m.toggleAll()
	case "g":
		m.onlyGlobal = !m.onlyGlobal
		m.cursor = 0
		m.rebuildView()
	case "r":
		return m.rescan()
	case "/":
		m.filtering = true
		m.filter.Focus()
		return m, textinput.Blink
	case "enter":
		if m.selectedCount() > 0 {
			m.state = stateConfirm
		}
	}
	return m, nil
}

func (m *Model) moveCursor(delta int) {
	if len(m.view) == 0 {
		return
	}
	m.cursor += delta
	if m.cursor < 0 {
		m.cursor = 0
	}
	if m.cursor >= len(m.view) {
		m.cursor = len(m.view) - 1
	}
	m.clampOffset()
}

func (m *Model) clampOffset() {
	if m.cursor < m.offset {
		m.offset = m.cursor
	}
	if m.cursor >= m.offset+m.height {
		m.offset = m.cursor - m.height + 1
	}
	if m.offset < 0 {
		m.offset = 0
	}
}

// toggleAll selects or deselects every item currently in view: if all are
// selected it deselects them, otherwise it selects them all.
func (m *Model) toggleAll() {
	allSelected := true
	for _, idx := range m.view {
		if !m.items[idx].selected {
			allSelected = false
			break
		}
	}
	for _, idx := range m.view {
		m.items[idx].selected = !allSelected
	}
}

// rebuildView recomputes the filtered/sorted view slice (indices into items).
func (m *Model) rebuildView() {
	q := strings.ToLower(strings.TrimSpace(m.filter.Value()))
	m.view = m.view[:0]
	for i, it := range m.items {
		if m.onlyGlobal && !it.target.Global {
			continue
		}
		if q != "" {
			hay := strings.ToLower(it.target.Path + " " + it.target.Kind)
			if !strings.Contains(hay, q) {
				continue
			}
		}
		m.view = append(m.view, i)
	}
	if m.allSized {
		sort.SliceStable(m.view, func(a, b int) bool {
			return m.items[m.view[a]].size > m.items[m.view[b]].size
		})
	}
	if m.cursor >= len(m.view) {
		m.cursor = max(0, len(m.view)-1)
	}
	m.clampOffset()
}

func (m Model) selectedCount() int {
	n := 0
	for _, it := range m.items {
		if it.selected {
			n++
		}
	}
	return n
}

func (m Model) selectedSize() int64 {
	var s int64
	for _, it := range m.items {
		if it.selected {
			s += it.size
		}
	}
	return s
}

// beginDelete snapshots the selected items into the delete queue and starts the
// first deletion.
func (m Model) beginDelete() (tea.Model, tea.Cmd) {
	m.delQueue = m.delQueue[:0]
	for i, it := range m.items {
		if it.selected {
			m.delQueue = append(m.delQueue, i)
		}
	}
	m.delIndex = 0
	m.freed = 0
	m.failures = nil
	m.state = stateDeleting
	if len(m.delQueue) == 0 {
		m.state = stateDone
		return m, nil
	}
	return m, m.deleteNext()
}

// deleteNext returns a command that deletes the current queued item.
func (m Model) deleteNext() tea.Cmd {
	it := m.items[m.delQueue[m.delIndex]]
	fn := m.deleteFn
	return func() tea.Msg {
		err := fn(it.target)
		return delMsg{path: it.target.Path, size: it.size, err: err}
	}
}

// Run launches the interactive TUI and blocks until the user exits, returning
// any deletion failures.
func Run(opts Options) (failed []wolf.Failure, err error) {
	m := New(opts)
	p := tea.NewProgram(m, tea.WithAltScreen())
	final, err := p.Run()
	if err != nil {
		return nil, err
	}
	if fm, ok := final.(Model); ok {
		return fm.failures, nil
	}
	return nil, nil
}
