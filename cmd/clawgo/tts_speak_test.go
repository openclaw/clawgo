package main

import (
	"context"
	"os/exec"
	"testing"
	"time"
)

func TestSystemTTSEngineSpeakHungProcessIsBoundedWhenTimeoutSet(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found")
	}

	engine := &systemTTSEngine{
		command: sleep,
		timeout: 200 * time.Millisecond,
	}
	start := time.Now()
	if err := engine.Speak(context.Background(), "2"); err == nil {
		t.Fatal("expected hung speak to return an error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timed speak took %s, want about 200ms", elapsed)
	}
}

func TestSystemTTSEngineSpeakUnlimitedKeepsHealthyShortUtterance(t *testing.T) {
	sleep, err := exec.LookPath("sleep")
	if err != nil {
		t.Skip("sleep not found")
	}

	engine := &systemTTSEngine{command: sleep}
	start := time.Now()
	if err := engine.Speak(context.Background(), "0.15"); err != nil {
		t.Fatalf("unlimited short speak: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 100*time.Millisecond {
		t.Fatalf("unlimited speak returned too early: %s", elapsed)
	}
}
