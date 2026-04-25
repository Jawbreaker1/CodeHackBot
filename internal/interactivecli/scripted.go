package interactivecli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/Jawbreaker1/CodeHackBot/internal/behavior"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerloop"
	"github.com/Jawbreaker1/CodeHackBot/internal/workerplan"
)

func usesTerminalTUI(r io.Reader, w io.Writer) bool {
	inFile, inOK := r.(*os.File)
	outFile, outOK := w.(*os.File)
	return inOK && outOK && isCharacterDevice(inFile) && isCharacterDevice(outFile)
}

func (s Shell) runScripted(ctx context.Context, frame behavior.Frame) error {
	reader := bufio.NewReader(s.Reader)
	ui := NewUIState()
	cwd, _ := os.Getwd()
	ui.SetEnvironment(s.Model, cwd, approvalStateLabel(s.AllowAll))
	flushFrom := 0
	flush := func() error {
		for flushFrom < len(ui.Stream) {
			if _, err := fmt.Fprintln(s.Writer, ui.Stream[flushFrom]); err != nil {
				return err
			}
			flushFrom++
		}
		return nil
	}
	if err := flush(); err != nil {
		return err
	}

	for {
		line, err := reader.ReadString('\n')
		if err != nil && err != io.EOF {
			return fmt.Errorf("read input: %w", err)
		}
		line = strings.TrimSpace(line)
		if line == "" {
			if err == io.EOF {
				return nil
			}
			continue
		}
		if line == "exit" || line == "quit" {
			return nil
		}
		if strings.HasPrefix(line, "/") {
			var cmdOut strings.Builder
			if handled, cmdErr := handleShellCommand(&cmdOut, line, ui.Started, ui.Packet); handled {
				ui.AddShellCommand(line, cmdOut.String(), cmdErr)
				if err := flush(); err != nil {
					return err
				}
				if err == io.EOF {
					return nil
				}
				continue
			}
		}
		s.applyUserInput(&ui, line)
		if err := flush(); err != nil {
			return err
		}

		modeDecision, classifyErr := s.classifyInput(ctx, frame, ui.Packet, line, ui.Started)
		modeDecision = s.applyClassification(&ui, modeDecision, classifyErr)
		if classifyErr != nil {
			if err := flush(); err != nil {
				return err
			}
		}
		if modeDecision.Mode == workerplan.ModeConversation {
			reply, convoErr := s.answerConversation(ctx, frame, ui.Packet, line, ui.Started)
			if err := s.applyConversationResult(&ui, line, reply, convoErr); err != nil {
				return fmt.Errorf("save session state after conversation: %w", err)
			}
			if err := flush(); err != nil {
				return err
			}
			if err == io.EOF {
				return nil
			}
			continue
		}

		packet, _, packetErr := s.prepareTaskPacket(ctx, frame, ui.Packet, ui.Started, line)
		if packetErr != nil {
			return packetErr
		}
		if err := s.applyTaskStart(&ui, packet, string(modeDecision.Mode)); err != nil {
			return fmt.Errorf("save session state: %w", err)
		}

		progressCh, resultCh := s.startWorkerRunAsync(ctx, ui.Packet)

		var outcome workerloop.Outcome
		var runErr error
		waiting := true
		for waiting {
			select {
			case progress, ok := <-progressCh:
				if !ok {
					progressCh = nil
					continue
				}
				if err := s.applyProgress(&ui, progress); err != nil {
					return fmt.Errorf("save progress state: %w", err)
				}
				if err := flush(); err != nil {
					return err
				}
			case result, ok := <-resultCh:
				if !ok {
					resultCh = nil
					continue
				}
				outcome = result.outcome
				runErr = result.err
				waiting = false
			}
		}
		if progressCh != nil {
			for progress := range progressCh {
				if err := s.applyProgress(&ui, progress); err != nil {
					return fmt.Errorf("save progress state: %w", err)
				}
				if err := flush(); err != nil {
					return err
				}
			}
		}
		if err := s.finalizeRun(ctx, &ui, outcome, runErr); err != nil {
			if runErr != nil {
				return fmt.Errorf("save session state after run error %q: %w", runErr, err)
			}
			return fmt.Errorf("save session state after run: %w", err)
		}
		if runErr != nil {
			if ctx.Err() != nil || errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded) {
				return runErr
			}
		}
		if err := flush(); err != nil {
			return err
		}
		if err == io.EOF {
			return nil
		}
	}
}
