// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ConflictItem represents a single merge conflict for the TUI.
type ConflictItem struct {
	Key        string
	BaseValue  string
	OurValue   string
	TheirValue string
	Resolved   bool
	Decision   MergeChoice
	EditValue  string
}

// MergeTUIResult is the output of the interactive merge TUI.
type MergeTUIResult struct {
	Conflicts []ConflictItem
	Aborted   bool
}

type mergeTUIModel struct {
	conflicts []ConflictItem
	cursor    int
	subCursor int // 0=accept, 1=reject, 2=edit, 3=skip
	editing   bool
	editBuf   string
	resolved  int
	quitting  bool
	aborted   bool
	height    int
}

func (m mergeTUIModel) Init() tea.Cmd { return nil }

func (m mergeTUIModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case tea.KeyMsg:
		if m.editing {
			return m.handleEditMode(msg)
		}
		return m.handleNormalMode(msg)
	}
	return m, nil
}

func (m mergeTUIModel) handleEditMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "enter":
		c := &m.conflicts[m.cursor]
		c.Decision = MergeEdit
		c.EditValue = m.editBuf
		c.Resolved = true
		m.resolved++
		m.editing = false
		m.advanceCursor()
		if m.allResolved() {
			m.quitting = true
			return m, tea.Quit
		}
	case "escape", "ctrl+c":
		m.editing = false
	case "backspace":
		if len(m.editBuf) > 0 {
			m.editBuf = m.editBuf[:len(m.editBuf)-1]
		}
	default:
		if len(msg.String()) == 1 {
			m.editBuf += msg.String()
		}
	}
	return m, nil
}

func (m mergeTUIModel) handleNormalMode(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
			m.subCursor = 0
		}
	case "down", "j":
		if m.cursor < len(m.conflicts)-1 {
			m.cursor++
			m.subCursor = 0
		}
	case "left", "h":
		if m.subCursor > 0 {
			m.subCursor--
		}
	case "right", "l":
		if m.subCursor < 3 {
			m.subCursor++
		}
	case "enter":
		c := &m.conflicts[m.cursor]
		if c.Resolved {
			return m, nil
		}
		switch m.subCursor {
		case 0: // Accept
			c.Decision = MergeAccept
			c.Resolved = true
			m.resolved++
		case 1: // Reject
			c.Decision = MergeReject
			c.Resolved = true
			m.resolved++
		case 2: // Edit
			m.editing = true
			m.editBuf = c.TheirValue
			return m, nil
		case 3: // Skip
			c.Decision = MergeSkip
			c.Resolved = true
			m.resolved++
		}
		m.advanceCursor()
		if m.allResolved() {
			m.quitting = true
			return m, tea.Quit
		}
	case "A": // Accept all remaining
		for i := range m.conflicts {
			if !m.conflicts[i].Resolved {
				m.conflicts[i].Decision = MergeAccept
				m.conflicts[i].Resolved = true
				m.resolved++
			}
		}
		m.quitting = true
		return m, tea.Quit
	case "R": // Reject all remaining
		for i := range m.conflicts {
			if !m.conflicts[i].Resolved {
				m.conflicts[i].Decision = MergeReject
				m.conflicts[i].Resolved = true
				m.resolved++
			}
		}
		m.quitting = true
		return m, tea.Quit
	case "ctrl+c", "q":
		m.aborted = true
		m.quitting = true
		return m, tea.Quit
	}
	return m, nil
}

func (m *mergeTUIModel) advanceCursor() {
	for i := m.cursor + 1; i < len(m.conflicts); i++ {
		if !m.conflicts[i].Resolved {
			m.cursor = i
			m.subCursor = 0
			return
		}
	}
}

func (m mergeTUIModel) allResolved() bool {
	return m.resolved >= len(m.conflicts)
}

func (m mergeTUIModel) View() string {
	if m.quitting {
		return ""
	}

	var sb strings.Builder

	// Header
	headerStyle := lipgloss.NewStyle().
		Foreground(ColorBrand).
		Bold(true).
		Padding(0, 1)

	progressStyle := lipgloss.NewStyle().
		Foreground(ColorMuted)

	sb.WriteString(headerStyle.Render("✦ Merge Conflict Resolution"))
	sb.WriteString("  ")
	sb.WriteString(progressStyle.Render(
		fmt.Sprintf("%d/%d resolved", m.resolved, len(m.conflicts))))
	sb.WriteString("\n\n")

	// Conflict list
	for i, c := range m.conflicts {
		isCurrent := i == m.cursor

		// Status indicator
		var status string
		if c.Resolved {
			switch c.Decision {
			case MergeAccept:
				status = StyleSuccess.Render("✓ accepted")
			case MergeReject:
				status = StyleWarning.Render("✗ rejected")
			case MergeEdit:
				status = StyleCode.Render("✎ edited")
			case MergeSkip:
				status = StyleDim.Render("- skipped")
			}
		} else {
			status = StyleDim.Render("• pending")
		}

		// Key line
		cursor := "  "
		keyStyle := StyleDim
		if isCurrent {
			cursor = StyleBrand.Render("▸ ")
			keyStyle = StyleBold
		}

		sb.WriteString(fmt.Sprintf("  %s%s  %s\n",
			cursor,
			keyStyle.Render(c.Key),
			status))

		// Show details for current conflict
		if isCurrent && !c.Resolved {
			sb.WriteString(fmt.Sprintf("      %s %s\n",
				StyleError.Render("ours:  "),
				MaskValue(c.OurValue)))
			sb.WriteString(fmt.Sprintf("      %s %s\n",
				StyleSuccess.Render("theirs:"),
				MaskValue(c.TheirValue)))

			if m.editing {
				sb.WriteString(fmt.Sprintf("      %s %s▋\n",
					StyleCode.Render("edit:  "),
					m.editBuf))
				sb.WriteString(StyleDim.Render("      Enter to confirm, Esc to cancel\n"))
			} else {
				// Action buttons
				sb.WriteString("      ")
				actions := []string{"Accept", "Reject", "Edit", "Skip"}
				for j, action := range actions {
					if j == m.subCursor {
						sb.WriteString(lipgloss.NewStyle().
							Foreground(ColorBrand).
							Bold(true).
							Underline(true).
							Render("[" + action + "]"))
					} else {
						sb.WriteString(StyleDim.Render("[" + action + "]"))
					}
					if j < len(actions)-1 {
						sb.WriteString(" ")
					}
				}
				sb.WriteString("\n")
			}
		}
		sb.WriteString("\n")
	}

	// Footer
	sb.WriteString(StyleDim.Render("  ↑↓ conflicts · ←→ actions · Enter select · A accept all · R reject all · q quit\n"))

	return sb.String()
}

// RunMergeTUI launches the full-screen interactive merge conflict resolver.
func RunMergeTUI(conflicts []ConflictItem) MergeTUIResult {
	if len(conflicts) == 0 {
		return MergeTUIResult{}
	}

	model := mergeTUIModel{
		conflicts: conflicts,
		height:    24,
	}

	p := tea.NewProgram(model, tea.WithAltScreen())
	finalModel, err := p.Run()
	if err != nil {
		return MergeTUIResult{Aborted: true}
	}

	m := finalModel.(mergeTUIModel)
	return MergeTUIResult{
		Conflicts: m.conflicts,
		Aborted:   m.aborted,
	}
}
