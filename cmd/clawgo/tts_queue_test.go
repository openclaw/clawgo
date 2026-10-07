package main

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

type nopTTSEngine struct{}

func (nopTTSEngine) Speak(context.Context, string) error { return nil }

func TestReplaceTTSQueueStopsPreviousLoop(t *testing.T) {
	first := newTTSQueue(nopTTSEngine{}, func(string, ...any) {})
	if first == nil {
		t.Fatal("newTTSQueue returned nil")
	}
	second := replaceTTSQueue(first, nopTTSEngine{}, func(string, ...any) {})
	if second == nil {
		t.Fatal("replaceTTSQueue returned nil")
	}
	t.Cleanup(second.Stop)

	select {
	case <-first.done:
	case <-time.After(2 * time.Second):
		t.Fatal("previous TTS queue still ranging after reconnect")
	}
}

func TestTTSQueueStopCancelsActiveSpeech(t *testing.T) {
	dir := t.TempDir()
	started := filepath.Join(dir, "started")
	script := filepath.Join(dir, "speak")
	if err := os.WriteFile(script, []byte("#!/bin/sh\necho started >> "+strconv.Quote(started)+"\nexec sleep 60\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	engine, err := newSystemTTSEngine(script, "", 0, 0)
	if err != nil {
		t.Fatal(err)
	}
	q := newTTSQueue(engine, func(string, ...any) {})
	t.Cleanup(q.Stop)
	q.Speak("first")
	deadline := time.Now().Add(5 * time.Second)
	for {
		if marker, err := os.ReadFile(started); err == nil && strings.TrimSpace(string(marker)) == "started" {
			break
		}
		if time.Now().After(deadline) {
			q.cancel()
			t.Fatal("speech did not start")
		}
		time.Sleep(time.Millisecond)
	}
	q.Speak("queued")
	done := make(chan struct{})
	go func() { q.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not reap active speech")
	}
	q.Stop()
	q.Speak("after stop")
	raw, err := os.ReadFile(started)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(string(raw)) != "started" {
		t.Fatalf("unexpected marker %q", raw)
	}
	select {
	case <-q.done:
	default:
		t.Fatal("queue worker still running")
	}
}
