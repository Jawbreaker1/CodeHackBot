package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/assist"
	"github.com/Jawbreaker1/CodeHackBot/internal/config"
	"github.com/Jawbreaker1/CodeHackBot/internal/memory"
)

func TestWriteAssistContextPacketAndReadMemoryOps(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-context-packet", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("ensure artifacts: %v", err)
	}
	input := assist.Input{
		Goal:        "identify iphone and scan non-intrusively",
		Scope:       []string{"internal"},
		Targets:     []string{"192.168.50.0/24"},
		Summary:     "summary",
		KnownFacts:  []string{"fact-a", "fact-b"},
		Focus:       "focus",
		ChatHistory: "User: hello",
		RecentLog:   "recent log",
		Plan:        "plan step",
		Inventory:   "inventory",
	}
	sections := r.buildAssistContextSections(sessionDir, artifacts, input)
	if err := r.writeAssistContextPacket(sessionDir, "goal text", "execute-step", input, sections); err != nil {
		t.Fatalf("writeAssistContextPacket: %v", err)
	}
	packetPath := assistContextPacketPath(sessionDir)
	data, err := os.ReadFile(packetPath)
	if err != nil {
		t.Fatalf("read context packet: %v", err)
	}
	var packet assistContextPacket
	if err := json.Unmarshal(data, &packet); err != nil {
		t.Fatalf("parse context packet: %v", err)
	}
	if packet.Mode != "execute-step" {
		t.Fatalf("mode mismatch: %q", packet.Mode)
	}
	if len(packet.Sections) == 0 {
		t.Fatalf("expected packet sections")
	}
	if err := r.appendAssistMemoryReadTrace(sessionDir, sections, "test_reason"); err != nil {
		t.Fatalf("appendAssistMemoryReadTrace: %v", err)
	}
	ops, err := r.recentAssistMemoryOps(sessionDir, 5)
	if err != nil {
		t.Fatalf("recentAssistMemoryOps: %v", err)
	}
	if len(ops) == 0 {
		t.Fatalf("expected memory ops")
	}
	if ops[len(ops)-1].Reason != "test_reason" {
		t.Fatalf("unexpected op reason: %q", ops[len(ops)-1].Reason)
	}
}

func TestHandleContextPacketPrintsPacketAndOps(t *testing.T) {
	cfg := config.Config{}
	cfg.Session.LogDir = t.TempDir()
	r := NewRunner(cfg, "session-context-packet-cmd", "", "")
	sessionDir, err := r.ensureSessionScaffold()
	if err != nil {
		t.Fatalf("ensure scaffold: %v", err)
	}
	artifacts, err := memory.EnsureArtifacts(sessionDir)
	if err != nil {
		t.Fatalf("ensure artifacts: %v", err)
	}
	input := assist.Input{
		Goal:       "test goal",
		Summary:    "summary",
		KnownFacts: []string{"fact-a"},
	}
	sections := r.buildAssistContextSections(sessionDir, artifacts, input)
	if err := r.writeAssistContextPacket(sessionDir, "test goal", "execute-step", input, sections); err != nil {
		t.Fatalf("writeAssistContextPacket: %v", err)
	}
	if err := r.appendAssistMemoryOp(sessionDir, assistMemoryOperation{
		Direction: "write",
		Component: "summary",
		Source:    "memory.summary",
		Path:      artifacts.SummaryPath,
		Chars:     7,
		Items:     1,
		Reason:    "test_write",
	}); err != nil {
		t.Fatalf("appendAssistMemoryOp: %v", err)
	}
	var out bytes.Buffer
	r.logger.SetOutput(&out)
	if err := r.handleCommand("/context packet"); err != nil {
		t.Fatalf("handleCommand /context packet: %v", err)
	}
	logs := out.String()
	if !containsAll(logs, "Context Packet:", "Context Packet Sections:", "Memory Ops") {
		t.Fatalf("unexpected /context packet output:\n%s", logs)
	}
}

func containsAll(text string, parts ...string) bool {
	for _, part := range parts {
		if part == "" {
			continue
		}
		if !bytes.Contains([]byte(text), []byte(part)) {
			return false
		}
	}
	return true
}
