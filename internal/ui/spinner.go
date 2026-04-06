// Copyright (c) EnvSync Contributors. SPDX-License-Identifier: MIT

package ui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// spinnerModel is a bubbletea model for an animated spinner.
type spinnerModel struct {
	spinner  spinner.Model
	message  string
	quitting bool
	err      error
	doneCh   chan struct{}
}

// spinnerDoneMsg signals the spinner to stop.
type spinnerDoneMsg struct{ err error }

func newSpinnerModel(message string, doneCh chan struct{}) spinnerModel {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(ColorBrand)
	return spinnerModel{
		spinner: s,
		message: message,
		doneCh:  doneCh,
	}
}

func (m spinnerModel) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, waitForDone(m.doneCh))
}

func waitForDone(doneCh chan struct{}) tea.Cmd {
	return func() tea.Msg {
		<-doneCh
		return spinnerDoneMsg{}
	}
}

func (m spinnerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			m.quitting = true
			return m, tea.Quit
		}
	case spinnerDoneMsg:
		m.quitting = true
		m.err = msg.err
		return m, tea.Quit
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m spinnerModel) View() string {
	if m.quitting {
		return ""
	}
	return fmt.Sprintf("  %s %s", m.spinner.View(), m.message)
}

// Spinner displays an animated bubbletea spinner.
type Spinner struct {
	message string
	doneCh  chan struct{}
	program *tea.Program
}

// NewSpinner creates a new bubbletea-powered spinner.
func NewSpinner(message string) *Spinner {
	return &Spinner{
		message: message,
		doneCh:  make(chan struct{}),
	}
}

// Start begins the spinner animation.
func (s *Spinner) Start() {
	model := newSpinnerModel(s.message, s.doneCh)
	s.program = tea.NewProgram(model, tea.WithOutput(nil))
	go func() {
		_, _ = s.program.Run()
	}()
}

// UpdateMessage changes the displayed message.
func (s *Spinner) UpdateMessage(msg string) {
	s.message = msg
}

// Stop halts the spinner.
func (s *Spinner) Stop() {
	select {
	case <-s.doneCh:
	default:
		close(s.doneCh)
	}
}

// StopWithMessage halts the spinner and shows a message.
func (s *Spinner) StopWithMessage(msg string) {
	s.Stop()
	printOutln(Indent(msg))
}

// StopSuccess shows a success message.
func (s *Spinner) StopSuccess(msg string) {
	s.StopWithMessage(SuccessIcon() + " " + msg)
}

// StopError shows an error message.
func (s *Spinner) StopError(msg string) {
	s.StopWithMessage(ErrorIcon() + " " + msg)
}

// WithSpinner runs a function with an animated spinner.
func WithSpinner(message string, fn func() error) error {
	sp := NewSpinner(message)
	sp.Start()
	err := fn()
	if err != nil {
		sp.StopError(err.Error())
	} else {
		sp.StopSuccess(message)
	}
	return err
}
