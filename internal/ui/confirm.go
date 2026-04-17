// Copyright (c) DevContract Contributors. SPDX-License-Identifier: MIT

package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// --- Confirm Prompt (Bubbletea) ---

type confirmModel struct {
	prompt     string
	defaultYes bool
	confirmed  bool
	decided    bool
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "y", "Y":
			m.confirmed = true
			m.decided = true
			return m, tea.Quit
		case "n", "N":
			m.confirmed = false
			m.decided = true
			return m, tea.Quit
		case "enter":
			m.confirmed = m.defaultYes
			m.decided = true
			return m, tea.Quit
		case "ctrl+c", "q":
			m.confirmed = false
			m.decided = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m confirmModel) View() string {
	hint := StyleDim.Render("[Y/n]")
	if !m.defaultYes {
		hint = StyleDim.Render("[y/N]")
	}
	return fmt.Sprintf("  %s %s %s ", InfoIcon(), m.prompt, hint)
}

// ConfirmAction asks the user for a yes/no confirmation using bubbletea.
func ConfirmAction(prompt string, defaultYes bool) bool {
	model := confirmModel{prompt: prompt, defaultYes: defaultYes}
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return defaultYes
	}
	return finalModel.(confirmModel).confirmed
}

// --- Merge Prompt (Bubbletea) ---

// MergeChoice represents the user's decision for a single variable.
type MergeChoice int

const (
	MergeAccept MergeChoice = iota
	MergeReject
	MergeEdit
	MergeSkip
)

// MergeDecision is the result of a per-variable merge prompt.
type MergeDecision struct {
	Key       string
	Choice    MergeChoice
	EditValue string
}

type mergePromptModel struct {
	key      string
	oldValue string
	newValue string
	cursor   int // 0=accept, 1=reject, 2=edit, 3=skip
	editing  bool
	editBuf  string
	decided  bool
	choice   MergeChoice
}

var mergeChoices = []string{"Accept (theirs)", "Reject (keep ours)", "Edit (custom)", "Skip"}

func (m mergePromptModel) Init() tea.Cmd { return nil }

func (m mergePromptModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.editing {
			switch msg.String() {
			case "enter":
				m.decided = true
				m.choice = MergeEdit
				return m, tea.Quit
			case "backspace":
				if len(m.editBuf) > 0 {
					m.editBuf = m.editBuf[:len(m.editBuf)-1]
				}
			case "ctrl+c":
				m.editing = false
			default:
				if len(msg.String()) == 1 {
					m.editBuf += msg.String()
				}
			}
			return m, nil
		}

		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(mergeChoices)-1 {
				m.cursor++
			}
		case "enter":
			switch m.cursor {
			case 0:
				m.choice = MergeAccept
				m.decided = true
				return m, tea.Quit
			case 1:
				m.choice = MergeReject
				m.decided = true
				return m, tea.Quit
			case 2:
				m.editing = true
				m.editBuf = m.newValue
				return m, nil
			case 3:
				m.choice = MergeSkip
				m.decided = true
				return m, tea.Quit
			}
		case "a":
			m.choice = MergeAccept
			m.decided = true
			return m, tea.Quit
		case "r":
			m.choice = MergeReject
			m.decided = true
			return m, tea.Quit
		case "s":
			m.choice = MergeSkip
			m.decided = true
			return m, tea.Quit
		case "ctrl+c", "q":
			m.choice = MergeSkip
			m.decided = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m mergePromptModel) View() string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(fmt.Sprintf("    %s %s\n",
		StyleBold.Render(m.key),
		StyleDim.Render("— conflict")))
	sb.WriteString(fmt.Sprintf("    %s  %s\n",
		StyleError.Render("ours: "),
		MaskValue(m.oldValue)))
	sb.WriteString(fmt.Sprintf("    %s  %s\n",
		StyleSuccess.Render("theirs:"),
		MaskValue(m.newValue)))
	sb.WriteString("\n")

	if m.editing {
		sb.WriteString(fmt.Sprintf("    %s %s▋\n",
			StyleDim.Render("value:"),
			m.editBuf))
		sb.WriteString(StyleDim.Render("    Enter to confirm, Ctrl+C to cancel\n"))
		return sb.String()
	}

	selectedStyle := lipgloss.NewStyle().Foreground(ColorBrand).Bold(true)

	for i, choice := range mergeChoices {
		cursor := "  "
		style := StyleDim
		if i == m.cursor {
			cursor = StyleBrand.Render("▸ ")
			style = selectedStyle
		}
		sb.WriteString(fmt.Sprintf("    %s%s\n", cursor, style.Render(choice)))
	}
	sb.WriteString(StyleDim.Render("\n    ↑↓ navigate · enter select · a/r/s shortcuts\n"))

	return sb.String()
}

// PromptMerge displays a per-variable merge prompt with bubbletea navigation.
func PromptMerge(key, oldValue, newValue string) MergeDecision {
	model := mergePromptModel{
		key:      key,
		oldValue: oldValue,
		newValue: newValue,
	}
	p := tea.NewProgram(model)
	finalModel, err := p.Run()
	if err != nil {
		return MergeDecision{Key: key, Choice: MergeSkip}
	}
	m := finalModel.(mergePromptModel)
	return MergeDecision{
		Key:       key,
		Choice:    m.choice,
		EditValue: m.editBuf,
	}
}

// PromptAcceptAll asks if the user wants to accept all remaining changes.
func PromptAcceptAll() bool {
	return ConfirmAction("Accept all remaining changes?", true)
}
