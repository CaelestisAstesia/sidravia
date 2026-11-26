package terminal

import (
	"errors"
	"io"
	"strings"

	"github.com/muesli/termenv"
)

// Presentation.go owns terminal output and is the only production file that
// imports termenv. It resolves terminal capability, owns Windows
// virtual-terminal enable/restore, styles trusted static labels and mapped
// state text, sanitizes dynamic values, and writes complete rendered blocks.
//
// Business operations continue to return errors and DTOs. Presentation never
// owns daemon discovery, IPC calls, password contents, Session state, retry
// decisions, persistence or protocol behavior. No termenv type crosses out of
// internal/cli.

const replacementRune = "�"

// ANSI color codes used exclusively for trusted static labels and mapped state
// text. Dynamic values never reach the color parser.
const (
	cyanColor   = "6"
	greenColor  = "2"
	redColor    = "1"
	yellowColor = "3"
)

// isControlRune reports whether r is a C0 control character, DEL, or a C1
// control character. These are the characters that can inject terminal control
// sequences or split output lines.
func isControlRune(r rune) bool {
	return r < 0x20 || (r >= 0x7f && r <= 0x9f)
}

// Sanitize replaces every C0/C1 control character and DEL in s with
// the replacement rune. Ordinary Unicode, spaces, punctuation, IDs, timestamps
// and Chinese text are preserved. Every value originating outside fixed CLI
// literals passes through this before output.
func Sanitize(s string) string {
	if !strings.ContainsFunc(s, isControlRune) {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isControlRune(r) {
			b.WriteString(replacementRune)
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

// Presentation bundles the resolved terminal capability, the writer and the
// Windows virtual-terminal restore function.
type Presentation struct {
	output  *termenv.Output
	profile termenv.Profile
	restore func() error
}

// resolveColorProfile is the terminal capability gate. The writer's actual
// ColorProfile is the first gate: when it is Ascii (a redirected or
// non-interactive writer) output stays plain regardless of CLICOLOR_FORCE.
// Only a color-capable real terminal is refined by EnvColorProfile, which
// honors NO_COLOR.
func resolveColorProfile(capability, envProfile termenv.Profile) termenv.Profile {
	if capability == termenv.Ascii {
		return termenv.Ascii
	}
	return envProfile
}

// resolveVTEnable decides the final profile and restore function after a
// Windows virtual-terminal enable attempt. An enable failure forces plain text
// and installs a safe no-op restore so no ANSI escape is emitted on that path.
// A successful enable keeps the resolved profile and the real restore function.
// The helper is pure: tests cover the decision without replacing the termenv
// function or adding package-global injection state.
func resolveVTEnable(profile termenv.Profile, restore func() error, enableErr error) (termenv.Profile, func() error) {
	if enableErr != nil {
		return termenv.Ascii, func() error { return nil }
	}
	if restore == nil {
		return profile, func() error { return nil }
	}
	return profile, restore
}

// NewPresentation resolves the terminal capability for w. It never forces
// ANSI in production: a redirected or non-interactive writer stays plain.
// Virtual-terminal enablement is optional; when it fails the command continues
// with plain text and a safe no-op restore, so no ANSI escape is emitted.
func NewPresentation(w io.Writer) *Presentation {
	out := termenv.NewOutput(w)
	profile := resolveColorProfile(out.ColorProfile(), out.EnvColorProfile())
	restore, enableErr := termenv.EnableVirtualTerminalProcessing(out)
	profile, restore = resolveVTEnable(profile, restore, enableErr)
	return &Presentation{output: out, profile: profile, restore: restore}
}

func PlainPresentation() *Presentation {
	out := termenv.NewOutput(io.Discard, termenv.WithProfile(termenv.Ascii), termenv.WithTTY(false))
	return &Presentation{output: out, profile: termenv.Ascii, restore: func() error { return nil }}
}

// close restores Windows virtual-terminal state if it was enabled. It must run
// after command output.
func (p *Presentation) Close() {
	_ = p.restore()
}

// write writes a fully rendered block to the writer. It rejects short writes.
func (p *Presentation) Write(block string) error {
	written, err := io.WriteString(p.output, block)
	if err != nil {
		return err
	}
	if written != len(block) {
		return io.ErrShortWrite
	}
	return nil
}

// complete writes a fully rendered block and then restores Windows
// virtual-terminal state. The restore always runs after the attempted write,
// even when the write failed. It returns the write error, the restore error,
// or both joined so errors.Is observes each cause.
func (p *Presentation) Complete(block string) error {
	writeErr := p.Write(block)
	restoreErr := p.restore()
	return errors.Join(writeErr, restoreErr)
}

// label styles a trusted static field label or section heading with bold cyan.
// The argument must be a fixed CLI literal, never a dynamic value.
func (p *Presentation) Label(text string) string {
	return p.profile.String(text).Foreground(p.profile.Color(cyanColor)).Bold().String()
}

// helpUsageLines renders the shared multi-line usage section. Every syntax is
// trusted static specification text and occupies its own two-space-indented
// line beneath the styled heading.
func (p *Presentation) HelpUsageLines(usages []string) []string {
	lines := []string{p.Label("用法：")}
	for _, usage := range usages {
		lines = append(lines, "  "+usage)
	}
	return lines
}

// state styles mapped Session-state text according to the stable state code.
// The code only selects the style; only the trusted mapped text is styled.
// suspended and unknown states render plain.
func (p *Presentation) State(mapped, code string) string {
	styled := p.profile.String(mapped)
	switch code {
	case "authenticated":
		styled = styled.Foreground(p.profile.Color(greenColor)).Bold()
	case "blocked_by_error":
		styled = styled.Foreground(p.profile.Color(redColor)).Bold()
	case "waiting_for_network", "authenticating", "waiting_before_retry", "stopping":
		styled = styled.Foreground(p.profile.Color(yellowColor))
	default:
		return mapped
	}
	return styled.String()
}

// ErrorLine renders the fatal CLI error line: the static Chinese prefix
// followed by the sanitized error message and one newline. An underlying
// wrapped cause is never printed; only the error's own message text is used.
func ErrorLine(err error) string {
	return "sidraviactl：错误：" + Sanitize(err.Error()) + "\n"
}
