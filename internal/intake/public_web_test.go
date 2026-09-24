package intake

import (
	"context"
	"net/http"
	"net/netip"
	"testing"

	"github.com/Jawbreaker1/CodeHackBot/internal/approval"
)

type captureDenial struct{ requests []approval.Request }

func (a *captureDenial) Approve(_ context.Context, request approval.Request) (approval.Decision, error) {
	a.requests = append(a.requests, request)
	return approval.DecisionDeny, nil
}

func TestPublicObservationRejectsInternalFetchTargets(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1/",
		"http://metadata.google.internal/",
		"http://example.com:8080/",
		"https://user:password@example.com/",
		"https://example.com/?token=secret",
		"file:///etc/passwd",
	} {
		if _, err := publicPageURL(raw); err == nil {
			t.Errorf("accepted unsafe page URL %q", raw)
		}
	}
	if parsed, err := publicPageURL("https://Example.com/"); err != nil || parsed.String() != "https://example.com/" {
		t.Fatalf("valid public page was rejected: url=%v err=%v", parsed, err)
	}
	for _, raw := range []string{"127.0.0.1", "localhost", "router.local", "192.168.1.1.nip.io:80"} {
		if _, err := publicDomain(raw); err == nil {
			t.Errorf("accepted non-public DNS hostname %q", raw)
		}
	}
	for _, raw := range []string{"127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "198.18.0.1", "::1", "fc00::1"} {
		if publicAddress(netip.MustParseAddr(raw)) {
			t.Errorf("accepted non-public address %q", raw)
		}
	}
	if !publicAddress(netip.MustParseAddr("1.1.1.1")) || !publicAddress(netip.MustParseAddr("2606:4700:4700::1111")) {
		t.Fatal("public unicast addresses were rejected")
	}
}

func TestAirGappedIntakeRejectsExternalObservationBeforeApproval(t *testing.T) {
	inspection := Inspection{Workspace: t.TempDir(), EvidenceDir: t.TempDir()}
	for _, call := range []ToolCall{{Name: "dns_lookup", Host: "example.com"}, {Name: "web_fetch", URL: "https://example.com/"}} {
		result, err := inspection.Run(context.Background(), call)
		if err != nil || result.Status != "failed" || result.Error == "" || result.EvidenceRef != "" {
			t.Fatalf("air-gapped call=%+v result=%+v err=%v", call, result, err)
		}
	}
}

func TestConnectedObservationShowsExactTargetBeforeContact(t *testing.T) {
	approver := &captureDenial{}
	inspection := Inspection{Workspace: t.TempDir(), EvidenceDir: t.TempDir(), Connected: true, Approver: approver}
	for _, test := range []struct {
		call   ToolCall
		target string
	}{
		{ToolCall{Name: "dns_lookup", Host: "example.com"}, "example.com"},
		{ToolCall{Name: "web_fetch", URL: "https://example.com/"}, "https://example.com/"},
	} {
		result, err := inspection.Run(context.Background(), test.call)
		if err != nil || result.Status != "denied" || result.EvidenceRef != "" {
			t.Fatalf("call=%+v result=%+v err=%v", test.call, result, err)
		}
		request := approver.requests[len(approver.requests)-1]
		if request.Target != test.target || request.Summary == "" || request.Impact == "" || request.Risk != "low" {
			t.Fatalf("approval request=%+v", request)
		}
	}
}

func TestPageMetadataOmitsCookies(t *testing.T) {
	headers := http.Header{"Content-Type": {"text/html"}, "Set-Cookie": {"session=secret"}, "Server": {"test-server"}}
	result := boundedHeaders(headers)
	if result["Content-Type"] != "text/html" || result["Server"] != "test-server" {
		t.Fatalf("useful page metadata missing: %+v", result)
	}
	if _, present := result["Set-Cookie"]; present {
		t.Fatalf("cookie leaked into observation: %+v", result)
	}
}
