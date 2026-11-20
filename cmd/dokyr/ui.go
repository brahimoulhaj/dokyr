package main

import (
	"fmt"
	"image/color"
	"io"
	"os"
	"strings"

	"charm.land/huh/v2"
	"charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/table"
	"golang.org/x/term"
)

// streams is where a command reads and writes. interactive says whether a person is
// there: prompts, spinners and colours are for a terminal, and a pipe or a script gets
// plain text that stays the same from one version to the next.
type streams struct {
	in          io.Reader
	out, errOut io.Writer
	interactive bool
	dark        bool // the terminal's background, for colours that read on both
}

func terminalStreams() streams {
	s := streams{in: os.Stdin, out: os.Stdout, errOut: os.Stderr}
	s.interactive = term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) && os.Getenv("TERM") != "dumb"
	if s.interactive {
		s.dark = lipgloss.HasDarkBackground(os.Stdin, os.Stdout)
	}
	return s
}

// palette is the panel's own colours (web/src/styles.css), with darker ones for a
// light terminal.
type palette struct{ accent, muted, danger, warn, line color.Color }

func (s streams) palette() palette {
	pick := lipgloss.LightDark(s.dark)
	return palette{
		accent: pick(lipgloss.Color("#4d7c0f"), lipgloss.Color("#9ece6a")),
		muted:  pick(lipgloss.Color("#6b7280"), lipgloss.Color("#7f8798")),
		danger: pick(lipgloss.Color("#be123c"), lipgloss.Color("#f7768e")),
		warn:   pick(lipgloss.Color("#a16207"), lipgloss.Color("#e0af68")),
		line:   pick(lipgloss.Color("#d1d5db"), lipgloss.Color("#3a4150")),
	}
}

// say prints a line to a person. Colours are dropped by lipgloss where the terminal
// has none (NO_COLOR, a 16-colour terminal gets the nearest ones).
func (s streams) say(format string, args ...any) {
	lipgloss.Fprintln(s.out, fmt.Sprintf(format, args...))
}

// done is the last line of a command that changed something.
func (s streams) done(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if !s.interactive {
		fmt.Fprintln(s.out, msg)
		return
	}
	p := s.palette()
	lipgloss.Fprintln(s.out, lipgloss.NewStyle().Foreground(p.accent).Bold(true).Render("✓ ")+msg)
}

func (s streams) accent(text string) string {
	if !s.interactive {
		return text
	}
	return lipgloss.NewStyle().Foreground(s.palette().accent).Bold(true).Render(text)
}

func (s streams) muted(text string) string {
	if !s.interactive {
		return text
	}
	return lipgloss.NewStyle().Foreground(s.palette().muted).Render(text)
}

// styled is text in a colour, for a terminal.
func (s streams) styled(c color.Color, bold bool, text string) string {
	if !s.interactive {
		return text
	}
	return lipgloss.NewStyle().Foreground(c).Bold(bold).Render(text)
}

// fail prints the error a command ended with.
func (s streams) fail(prefix string, err error) {
	if !s.interactive {
		fmt.Fprintln(s.errOut, prefix+":", err)
		return
	}
	lipgloss.Fprintln(s.errOut, lipgloss.NewStyle().Foreground(s.palette().danger).Bold(true).Render("✗ ")+err.Error())
}

// card is a framed block, for the one thing on screen the user must read.
func (s streams) card(lines ...string) string {
	return lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(s.palette().line).
		Padding(0, 2).
		Render(strings.Join(lines, "\n"))
}

// table is a framed table for a terminal. cellColor may colour a cell by its content
// (nil for none).
func (s streams) table(headers []string, rows [][]string, cellColor func(col int, value string) color.Color) string {
	p := s.palette()
	header := lipgloss.NewStyle().Foreground(p.accent).Bold(true).Padding(0, 1)
	cell := lipgloss.NewStyle().Padding(0, 1)
	t := table.New().
		Border(lipgloss.RoundedBorder()).
		BorderStyle(lipgloss.NewStyle().Foreground(p.line)).
		BorderColumn(false).
		Headers(headers...).
		Rows(rows...).
		StyleFunc(func(row, col int) lipgloss.Style {
			if row == table.HeaderRow {
				return header
			}
			if cellColor != nil && row >= 0 && row < len(rows) && col < len(rows[row]) {
				if c := cellColor(col, rows[row][col]); c != nil {
					return cell.Foreground(c)
				}
			}
			return cell
		})
	// Wider than the terminal, the frame would wrap into noise: the table is cut to
	// the width instead, and long cells end in an ellipsis.
	if f, ok := s.out.(*os.File); ok {
		if width, _, err := term.GetSize(int(f.Fd())); err == nil && width > 20 && lipgloss.Width(t.String()) > width {
			t = t.Width(width).Wrap(false)
		}
	}
	return t.String()
}

// theme is the look of prompts and spinners: huh's base with the panel's accent.
func (s streams) theme() huh.Theme {
	return huh.ThemeFunc(func(isDark bool) *huh.Styles {
		p := streams{dark: isDark}.palette()
		t := huh.ThemeBase(isDark)
		t.Focused.Base = t.Focused.Base.BorderForeground(p.accent)
		t.Focused.Title = t.Focused.Title.Foreground(p.accent).Bold(true)
		t.Focused.Description = t.Focused.Description.Foreground(p.muted)
		t.Focused.ErrorIndicator = t.Focused.ErrorIndicator.Foreground(p.danger)
		t.Focused.ErrorMessage = t.Focused.ErrorMessage.Foreground(p.danger)
		t.Focused.SelectSelector = t.Focused.SelectSelector.Foreground(p.accent)
		t.Focused.SelectedOption = t.Focused.SelectedOption.Foreground(p.accent)
		t.Focused.TextInput.Cursor = t.Focused.TextInput.Cursor.Foreground(p.accent)
		t.Focused.TextInput.Prompt = t.Focused.TextInput.Prompt.Foreground(p.accent)
		t.Focused.TextInput.Placeholder = t.Focused.TextInput.Placeholder.Foreground(p.muted)
		t.Blurred = t.Focused
		t.Blurred.Base = t.Focused.Base.BorderStyle(lipgloss.HiddenBorder())
		t.Blurred.Title = t.Blurred.Title.Foreground(p.muted).Bold(false)
		return t
	})
}

// ask runs a form in the terminal. Leaving it (Ctrl+C, Esc) is not an error to explain.
func (s streams) ask(fields ...huh.Field) error {
	err := huh.NewForm(huh.NewGroup(fields...)).WithTheme(s.theme()).Run()
	if err == huh.ErrUserAborted {
		return errCancelled
	}
	return err
}
