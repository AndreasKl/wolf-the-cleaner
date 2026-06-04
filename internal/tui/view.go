package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"it.kluth.buildcleaner/internal/wolf"
)

var (
	titleStyle   = lipgloss.NewStyle().Bold(true)
	dimStyle     = lipgloss.NewStyle().Faint(true)
	accentStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	selMarkStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("84"))
	cursorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Bold(true)
	failureStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("203"))
)

// View renders the model for the current state.
func (m Model) View() string {
	switch m.state {
	case stateScanning:
		return fmt.Sprintf("\n  %s scanning %s ...\n", m.spinner.View(), m.opts.Root)
	case stateConfirm:
		verb := "Move"
		suffix := " to the trash"
		if m.opts.Permanent {
			verb = "Permanently delete"
			suffix = ""
		}
		return fmt.Sprintf(
			"\n  %s\n\n  %s %d directories (%s)%s?  [y] yes  [n] back\n",
			titleStyle.Render("Confirm"),
			verb, m.selectedCount(), wolf.FormatSize(m.selectedSize()), suffix,
		)
	case stateDeleting:
		done := m.delIndex
		total := len(m.delQueue)
		pct := 0.0
		if total > 0 {
			pct = float64(done) / float64(total)
		}
		action := "Trashing"
		if m.opts.Permanent {
			action = "Deleting"
		}
		return fmt.Sprintf("\n  %s %d/%d\n\n  %s\n", action, done, total, m.prog.ViewAs(pct))
	case stateDone:
		var b strings.Builder
		fmt.Fprintf(&b, "\n  %s\n\n", titleStyle.Render("Done"))
		verb := "Moved to trash"
		if m.opts.Permanent {
			verb = "Freed"
		}
		fmt.Fprintf(&b, "  %s: %s across %d directories\n", verb, wolf.FormatSize(m.freed), len(m.delQueue)-len(m.failures))
		for _, f := range m.failures {
			fmt.Fprintf(&b, "  %s %s: %v\n", failureStyle.Render("failed:"), f.Path, f.Err)
		}
		fmt.Fprint(&b, "\n  "+dimStyle.Render("[r/enter] rescan  [q] quit")+"\n")
		return b.String()
	default:
		return m.selectingView()
	}
}

func (m Model) selectingView() string {
	var b strings.Builder
	header := titleStyle.Render("Wolf the Cleaner")
	if m.onlyGlobal {
		header += dimStyle.Render("  [globals only]")
	}
	fmt.Fprintf(&b, "\n  %s\n\n", header)

	end := min(m.offset+m.height, len(m.view))
	for i := m.offset; i < end; i++ {
		it := m.items[m.view[i]]
		mark := " "
		if it.selected {
			mark = selMarkStyle.Render("x")
		}
		size := "    …"
		if it.sized {
			size = wolf.FormatSize(it.size)
		}
		row := fmt.Sprintf("[%s] %10s  %s  %s", mark, size, it.target.Path, dimStyle.Render("("+it.target.Kind+")"))
		if i == m.cursor {
			row = cursorStyle.Render("> ") + row
		} else {
			row = "  " + row
		}
		fmt.Fprintln(&b, "  "+row)
	}

	if m.filtering {
		fmt.Fprintf(&b, "\n  /%s\n", m.filter.View())
	}

	lo := 0
	if len(m.view) > 0 {
		lo = m.offset + 1
	}
	fmt.Fprintf(&b, "\n  %s   %s\n",
		accentStyle.Render(fmt.Sprintf("Selected: %s / %d dirs", wolf.FormatSize(m.selectedSize()), m.selectedCount())),
		dimStyle.Render(fmt.Sprintf("%d–%d of %d", lo, end, len(m.view))),
	)
	fmt.Fprint(&b, dimStyle.Render("  [space] toggle  [a] all  [g] globals  [/] filter  [r] rescan  [enter] delete  [q] quit")+"\n")
	return b.String()
}
