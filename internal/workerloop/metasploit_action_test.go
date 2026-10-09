package workerloop

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	ctxpacket "github.com/Jawbreaker1/CodeHackBot/internal/context"
	"github.com/Jawbreaker1/CodeHackBot/internal/execx"
)

func TestMetasploitLookupPreparesLocalMetadataAction(t *testing.T) {
	workspace := t.TempDir()
	logDir := t.TempDir()
	bin := t.TempDir()
	for _, name := range []string{"msfconsole", "env"} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	for _, tc := range []struct {
		name     string
		response Response
		query    string
	}{
		{"cve", Response{Type: "metasploit_lookup", CVE: "cve-2021-44228"}, "search cve:CVE-2021-44228; exit -y"},
		{"module", Response{Type: "metasploit_lookup", Module: "auxiliary/scanner/http/log4shell_scanner"}, "info auxiliary/scanner/http/log4shell_scanner; exit -y"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, failure := prepareMetasploitLookup(tc.response, workspace, logDir)
			if failure != nil {
				t.Fatalf("lookup rejected: %s", failure.OutputSummary)
			}
			if prepared.Command != "env" || len(prepared.Args) != 5 || prepared.Args[0] != "MSF_CFGROOT_CONFIG="+logDir || prepared.Args[1] != "msfconsole" || prepared.Args[4] != tc.query {
				t.Fatalf("unexpected invocation: %q %q", prepared.Command, prepared.Args)
			}
			if prepared.Risk != "low" || !strings.Contains(prepared.Impact, "no assessment target is contacted") || prepared.Target != "local Metasploit module catalog" {
				t.Fatalf("unclear lookup effect: %+v", prepared)
			}
		})
	}
	if !filepath.IsAbs(workspace) {
		t.Fatal("test workspace must be absolute")
	}
}

func TestMetasploitLookupRejectsConsoleInjection(t *testing.T) {
	for _, input := range []string{
		`{"type":"metasploit_lookup","module":"exploit/multi/http/example; run"}`,
		`{"type":"metasploit_lookup","module":"exploit/multi/http/../run"}`,
		`{"type":"metasploit_lookup","cve":"CVE-2021-44228; run"}`,
		`{"type":"metasploit_lookup","cve":"CVE-2021-44228","risk":"low"}`,
	} {
		if _, err := ParseResponse(input); err == nil {
			t.Fatalf("accepted unsafe lookup: %s", input)
		}
	}
}

func TestMetasploitLookupUsesApprovalAndEvidence(t *testing.T) {
	bin := t.TempDir()
	// A fake local catalog keeps the execution contract test independent of
	// whether Metasploit is installed in CI.
	script := "#!/bin/sh\nprintf 'config=%s\\n' \"$MSF_CFGROOT_CONFIG\"\nprintf 'argument=%s\\n' \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "msfconsole"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	workspace := t.TempDir()
	logDir := t.TempDir()
	approver := &recordingApprover{}
	loop := Loop{Executor: execx.Executor{LogDir: logDir}, Approver: approver}
	packet := ctxpacket.WorkerPacket{}
	packet.OperatorState.WorkingDir = workspace
	executed, err := loop.execute(context.Background(), &packet, Response{Type: "metasploit_lookup", CVE: "CVE-2021-44228"})
	if err != nil || !executed {
		t.Fatalf("lookup did not execute: executed=%v err=%v", executed, err)
	}
	if approver.request.Risk != "low" || approver.request.Target != "local Metasploit module catalog" || !strings.Contains(approver.request.Command, "search cve:CVE-2021-44228; exit -y") {
		t.Fatalf("approval did not describe the lookup: %+v", approver.request)
	}
	result := packet.LatestExecutionResult
	if result.ExitStatus != "0" || !strings.Contains(result.OutputEvidence, "config="+logDir) || !strings.Contains(result.OutputEvidence, "argument=search cve:CVE-2021-44228; exit -y") || len(result.LogRefs) == 0 {
		t.Fatalf("lookup did not retain exact local evidence: %+v", result)
	}
}
