package defaultpolicy

import (
	"context"
	"testing"

	"github.com/clawdbot/clawgo/internal/routing"
)

type call struct {
	sessionKey string
	text       string
	deliver    bool
	channel    string
	to         string
	provider   string
	message    string
}

type fakeTransport struct {
	voice    []call
	agent    []call
	provider []call
}

func (f *fakeTransport) SendVoiceTranscript(sessionKey, text string) error {
	f.voice = append(f.voice, call{sessionKey: sessionKey, text: text})
	return nil
}

func (f *fakeTransport) SendAgentRequest(sessionKey, text string, deliver bool, channel, to string) error {
	f.agent = append(f.agent, call{sessionKey: sessionKey, text: text, deliver: deliver, channel: channel, to: to})
	return nil
}

func (f *fakeTransport) SendProviderMessage(provider, to, message string) error {
	f.provider = append(f.provider, call{provider: provider, to: to, message: message})
	return nil
}

func TestDefaultPolicyQuickAction(t *testing.T) {
	transport := &fakeTransport{}
	cfg := routing.Config{
		QuickActions:     true,
		DeliverChannel:   "telegram",
		DeliverTo:        "123",
		QuickPingMessage: "Ping.",
	}
	policy, err := New(cfg, transport, func(string, ...any) {})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	handled, err := policy.HandleTranscript(context.Background(), "hey razor ping me on telegram")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled")
	}
	if len(transport.provider) != 1 {
		t.Fatalf("expected provider message")
	}
	if len(transport.voice) != 0 || len(transport.agent) != 0 {
		t.Fatalf("unexpected fallback sends")
	}
}

func TestDefaultPolicyAgentRequest(t *testing.T) {
	transport := &fakeTransport{}
	cfg := routing.Config{
		SessionKey:     "main",
		AgentRequest:   true,
		Deliver:        true,
		DeliverChannel: "telegram",
		DeliverTo:      "123",
	}
	policy, err := New(cfg, transport, func(string, ...any) {})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	_, err = policy.HandleTranscript(context.Background(), "hello")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(transport.agent) != 1 {
		t.Fatalf("expected agent request")
	}
	if len(transport.voice) != 0 || len(transport.provider) != 0 {
		t.Fatalf("unexpected fallback sends")
	}
}

func TestDefaultPolicyVoiceTranscript(t *testing.T) {
	transport := &fakeTransport{}
	cfg := routing.Config{SessionKey: "main"}
	policy, err := New(cfg, transport, func(string, ...any) {})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	_, err = policy.HandleTranscript(context.Background(), "hello")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if len(transport.voice) != 1 {
		t.Fatalf("expected voice transcript")
	}
	if len(transport.agent) != 0 || len(transport.provider) != 0 {
		t.Fatalf("unexpected fallback sends")
	}
}

func TestDefaultPolicyEmptyTranscript(t *testing.T) {
	transport := &fakeTransport{}
	cfg := routing.Config{SessionKey: "main"}
	policy, err := New(cfg, transport, func(string, ...any) {})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	handled, err := policy.HandleTranscript(context.Background(), "   ")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if handled {
		t.Fatalf("expected not handled for empty transcript")
	}
	if len(transport.voice) != 0 || len(transport.agent) != 0 || len(transport.provider) != 0 {
		t.Fatalf("expected no sends for empty transcript")
	}
}

func TestDefaultPolicyQuickActionWithoutTelegramConfig(t *testing.T) {
	transport := &fakeTransport{}
	cfg := routing.Config{
		QuickActions:   true,
		DeliverChannel: "",
		DeliverTo:      "",
	}
	policy, err := New(cfg, transport, func(string, ...any) {})
	if err != nil {
		t.Fatalf("new policy: %v", err)
	}
	handled, err := policy.HandleTranscript(context.Background(), "please ping telegram")
	if err != nil {
		t.Fatalf("handle: %v", err)
	}
	if !handled {
		t.Fatalf("expected handled")
	}
	if len(transport.provider) != 0 || len(transport.voice) != 0 || len(transport.agent) != 0 {
		t.Fatalf("expected quick action to short-circuit without sends when telegram is not configured")
	}
}
