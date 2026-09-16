package main

import "testing"

func TestChatSessionDefaultsToOutgoingSession(t *testing.T) {
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{name: "default", want: "main"},
		{name: "outgoing", args: []string{"-session-key", "other"}, want: "other"},
		{name: "explicit", args: []string{"-session-key", "other", "-chat-session-key", "main"}, want: "main"},
		{name: "empty", args: []string{"-session-key", "other", "-chat-session-key", ""}, want: "other"},
		{name: "trimmed", args: []string{"-session-key", " other ", "-chat-session-key", " "}, want: "other"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := parseFlags("run", tc.args)
			if cfg.ChatSessionKey != tc.want {
				t.Fatalf("chat session=%q, want %q", cfg.ChatSessionKey, tc.want)
			}
		})
	}
}
