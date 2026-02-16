package cli

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"
)

const (
	escKey   = 0x1b
	ctrlCKey = 0x03
)

func startInterruptWatcher() (<-chan struct{}, func(), error) {
	cancelCh := make(chan struct{}, 1)
	done := make(chan struct{})
	var once sync.Once
	stop := func() { once.Do(func() { close(done) }) }

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	go func() {
		defer signal.Stop(sigCh)
		for {
			select {
			case <-done:
				return
			case <-sigCh:
				notifyInterrupt(cancelCh)
				return
			}
		}
	}()

	keyErr := startKeyWatcher(cancelCh, done)
	return cancelCh, stop, keyErr
}

func startKeyWatcher(cancelCh chan<- struct{}, done <-chan struct{}) error {
	if runtime.GOOS == "windows" {
		return fmt.Errorf("ESC interrupt not supported on windows")
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
		return fmt.Errorf("no TTY available for ESC")
	}
	state, err := term.MakeRaw(fd)
	if err != nil {
		return fmt.Errorf("raw mode failed: %w", err)
	}
	if err := syscall.SetNonblock(fd, true); err != nil {
		_ = term.Restore(fd, state)
		return fmt.Errorf("set nonblock failed: %w", err)
	}

	cleanup := func() {
		_ = term.Restore(fd, state)
		_ = syscall.SetNonblock(fd, false)
		if tty != os.Stdin {
			_ = tty.Close()
		}
	}

	go func() {
		defer cleanup()
		buf := make([]byte, 1)
		for {
			select {
			case <-done:
				return
			default:
			}
			n, err := tty.Read(buf)
			if n > 0 && (buf[0] == escKey || buf[0] == ctrlCKey) {
				notifyInterrupt(cancelCh)
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

	return nil
}

func notifyInterrupt(cancelCh chan<- struct{}) {
	select {
	case cancelCh <- struct{}{}:
	default:
	}
}
