package cli

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

const escKey = 0x1b

func startEscWatcher() (<-chan struct{}, func(), error) {
	if runtime.GOOS == "windows" {
		return nil, func() {}, fmt.Errorf("ESC interrupt not supported on windows")
	}

	tty, err := os.OpenFile("/dev/tty", os.O_RDONLY, 0)
	if err != nil {
		tty = os.Stdin
	}

	fd := int(tty.Fd())
	if !term.IsTerminal(fd) {
		if tty != os.Stdin {
			_ = tty.Close()
		}
		return nil, func() {}, fmt.Errorf("no TTY available for ESC")
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return nil, func() {}, fmt.Errorf("raw mode failed: %w", err)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = term.Restore(fd, state)
		return nil, func() {}, fmt.Errorf("set nonblock failed: %w", err)
	}

	escCh := make(chan struct{}, 1)
	done := make(chan struct{})
	var once sync.Once
	stop := func() {
		once.Do(func() {
			close(done)
		})
	}

	go func() {
		defer func() {
			_ = term.Restore(fd, state)
			_ = syscall.SetNonblock(fd, false)
			if tty != os.Stdin {
				_ = tty.Close()
			}
		}()
		buf := make([]byte, 1)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, err := tty.Read(buf)
			if n > 0 && buf[0] == escKey {
				escCh <- struct{}{}
				return
			}
			if err != nil {
				if errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) {
					time.Sleep(50 * time.Millisecond)
					continue
				}
				return
			}
		}
	}()

	return escCh, stop, nil
}
