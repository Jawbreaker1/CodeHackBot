package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"golang.org/x/term"
)

const (
	inputLineStyleStart  = "\x1b[48;5;236m\x1b[38;5;252m"
	statusLineStyleStart = "\x1b[48;5;238m\x1b[38;5;250m"
	inputLineStyleReset  = "\x1b[0m"
)

func (r *Runner) readLine(prompt string) (string, error) {
	if r.isTTY() {
		return r.readLineInteractive(prompt)
	}
	if prompt != "" {
		safePrint(prompt)
	}
	line, err := r.reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return "", err
	}
	return strings.TrimSpace(line), err
}

func (r *Runner) readLineInteractive(prompt string) (string, error) {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		if prompt != "" {
			safePrint(prompt)
		}
		line, err := r.reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return "", err
		}
		return strings.TrimSpace(line), err
	}

	state, err := term.MakeRaw(fd)
	if err != nil {
		if prompt != "" {
			safePrint(prompt)
		}
		line, readErr := r.reader.ReadString('\n')
		if readErr != nil && readErr != io.EOF {
			return "", readErr
		}
		return strings.TrimSpace(line), readErr
	}
	defer func() {
		_ = term.Restore(fd, state)
	}()

	buf := make([]byte, 0, 128)
	recordHistory := strings.HasPrefix(prompt, "BirdHackBot")
	r.historyIndex = -1
	r.historyScratch = ""
	statusLine := r.promptStatusLine()

	r.redrawInputLine(prompt, buf, statusLine)

	for {
		b, readErr := r.reader.ReadByte()
		if readErr != nil {
			return "", readErr
		}
		switch b {
		case '\r', '\n':
			r.clearInputRender()
			safePrint("\r\n")
			r.inputRenderLines = 0
			line := strings.TrimSpace(string(buf))
			if recordHistory && line != "" {
				if len(r.history) == 0 || r.history[len(r.history)-1] != line {
					r.history = append(r.history, line)
				}
			}
			r.historyIndex = -1
			r.historyScratch = ""
			return line, nil
		case 0x03:
			r.clearInputRender()
			safePrint("\r\n")
			r.inputRenderLines = 0
			r.historyIndex = -1
			r.historyScratch = ""
			return "", io.EOF
		case 0x04:
			if len(buf) == 0 {
				r.clearInputRender()
				safePrint("\r\n")
				r.inputRenderLines = 0
				r.historyIndex = -1
				r.historyScratch = ""
				return "", io.EOF
			}
		case 0x7f, 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				r.redrawInputLine(prompt, buf, statusLine)
			}
			continue
		case 0x1b:
			seq1, seqErr := r.reader.ReadByte()
			if seqErr != nil {
				continue
			}
			if seq1 != '[' {
				continue
			}
			seq2, seqErr := r.reader.ReadByte()
			if seqErr != nil {
				continue
			}
			switch seq2 {
			case 'A':
				if len(r.history) == 0 {
					continue
				}
				if r.historyIndex == -1 {
					r.historyScratch = string(buf)
					r.historyIndex = len(r.history) - 1
				} else if r.historyIndex > 0 {
					r.historyIndex--
				}
				buf = []byte(r.history[r.historyIndex])
				r.redrawInputLine(prompt, buf, statusLine)
			case 'B':
				if r.historyIndex == -1 {
					continue
				}
				if r.historyIndex < len(r.history)-1 {
					r.historyIndex++
					buf = []byte(r.history[r.historyIndex])
				} else {
					r.historyIndex = -1
					buf = []byte(r.historyScratch)
					r.historyScratch = ""
				}
				r.redrawInputLine(prompt, buf, statusLine)
			}
			continue
		default:
			if b >= 32 && b != 127 {
				buf = append(buf, b)
				r.redrawInputLine(prompt, buf, statusLine)
			}
		}
	}
}

func (r *Runner) redrawInputLine(prompt string, buf []byte, statusLine string) {
	input := fitSingleLine(prompt+string(buf), terminalWidth())
	status := padOrTrimSingleLine(statusLine, terminalWidth())
	withOutputLock(func() {
		r.clearInputRenderLocked()
		_, _ = fmt.Fprintf(os.Stdout, "%s%s\x1b[K%s", inputLineStyleStart, input, inputLineStyleReset)
		_, _ = fmt.Fprint(os.Stdout, "\x1b[s")
		_, _ = fmt.Fprintf(os.Stdout, "\x1b[1B\r%s%s\x1b[K%s", statusLineStyleStart, status, inputLineStyleReset)
		_, _ = fmt.Fprint(os.Stdout, "\x1b[u")
	})
	r.inputRenderLines = 2
}

func (r *Runner) clearInputRender() {
	withOutputLock(func() {
		r.clearInputRenderLocked()
	})
}

func (r *Runner) clearInputRenderLocked() {
	if r.inputRenderLines <= 0 {
		return
	}
	if r.inputRenderLines == 2 {
		_, _ = fmt.Fprint(os.Stdout, "\r\x1b[2K")
		_, _ = fmt.Fprint(os.Stdout, "\x1b[1B\r\x1b[2K")
		_, _ = fmt.Fprint(os.Stdout, "\x1b[1A\r")
		return
	}
	for i := 0; i < r.inputRenderLines; i++ {
		_, _ = fmt.Fprint(os.Stdout, "\r\x1b[2K")
		if i < r.inputRenderLines-1 {
			_, _ = fmt.Fprint(os.Stdout, "\x1b[1A")
		}
	}
	_, _ = fmt.Fprint(os.Stdout, "\r")
}

func terminalWidth() int {
	width, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || width <= 0 {
		return 80
	}
	return width
}

func visualLineCount(text string, width int) int {
	if width <= 0 {
		width = 80
	}
	runes := len([]rune(text))
	if runes <= 0 {
		return 1
	}
	lines := runes / width
	if runes%width != 0 {
		lines++
	}
	if lines <= 0 {
		return 1
	}
	return lines
}

func fitSingleLine(text string, width int) string {
	max := maxDisplayColumns(width)
	runes := []rune(text)
	if len(runes) <= max {
		return string(runes)
	}
	if max <= 3 {
		return string(runes[len(runes)-max:])
	}
	return "..." + string(runes[len(runes)-(max-3):])
}

func padOrTrimSingleLine(text string, width int) string {
	max := maxDisplayColumns(width)
	line := fitSingleLine(strings.TrimSpace(text), width)
	runeCount := utf8.RuneCountInString(line)
	if runeCount >= max {
		return line
	}
	return line + strings.Repeat(" ", max-runeCount)
}

func maxDisplayColumns(width int) int {
	if width <= 0 {
		width = 80
	}
	if width > 1 {
		return width - 1
	}
	return 1
}
