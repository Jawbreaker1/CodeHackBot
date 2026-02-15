package cli

import (
	"fmt"
	"io"
	"os"
	"strings"

	"golang.org/x/term"
)

const (
	inputLineStyleStart = "\x1b[48;5;236m\x1b[38;5;252m"
	inputLineStyleReset = "\x1b[0m"
)

func (r *Runner) readLine(prompt string) (string, error) {
	if prompt != "" && r.isTTY() && strings.HasPrefix(prompt, "BirdHackBot") {
		return r.readLineInteractive(prompt)
	}
	if prompt != "" {
		fmt.Print(prompt)
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
			fmt.Print(prompt)
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
			fmt.Print(prompt)
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

	r.redrawInputLine(prompt, buf)

	for {
		b, readErr := r.reader.ReadByte()
		if readErr != nil {
			return "", readErr
		}
		switch b {
		case '\r', '\n':
			fmt.Print("\r\n")
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
			fmt.Print("\r\n")
			r.inputRenderLines = 0
			r.historyIndex = -1
			r.historyScratch = ""
			return "", io.EOF
		case 0x04:
			if len(buf) == 0 {
				fmt.Print("\r\n")
				r.inputRenderLines = 0
				r.historyIndex = -1
				r.historyScratch = ""
				return "", io.EOF
			}
		case 0x7f, 0x08:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				r.redrawInputLine(prompt, buf)
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
				r.redrawInputLine(prompt, buf)
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
				r.redrawInputLine(prompt, buf)
			}
			continue
		default:
			if b >= 32 && b != 127 {
				buf = append(buf, b)
				r.redrawInputLine(prompt, buf)
			}
		}
	}
}

func (r *Runner) redrawInputLine(prompt string, buf []byte) {
	r.clearInputRender()
	fmt.Printf("%s%s%s%s", inputLineStyleStart, prompt, string(buf), inputLineStyleReset)
	r.inputRenderLines = visualLineCount(prompt+string(buf), terminalWidth())
}

func (r *Runner) clearInputRender() {
	if r.inputRenderLines <= 0 {
		return
	}
	for i := 0; i < r.inputRenderLines; i++ {
		fmt.Print("\r\x1b[2K")
		if i < r.inputRenderLines-1 {
			fmt.Print("\x1b[1A")
		}
	}
	fmt.Print("\r")
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
