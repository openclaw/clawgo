package stt

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

func TestBrabbleProcess(t *testing.T) {
	if len(os.Args) < 2 {
		return
	}
	switch os.Args[len(os.Args)-1] {
	case "brabble-output":
		for i := 0; i < 500; i++ {
			fmt.Printf("%03d %s\n", i, strings.Repeat("x", 100))
		}
		os.Exit(0)
	case "brabble-stderr":
		fmt.Fprintln(os.Stderr, strings.Repeat("x", 9*1024*1024))
		fmt.Println("complete")
		os.Exit(0)
	}
}

func TestBrabbleCancellationUnblocksOutput(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	engine := NewBrabbleEngine(BrabbleConfig{Command: executable, Args: []string{"-test.run=^TestBrabbleProcess$", "--", "brabble-output"}}, nil)
	out, err := engine.Transcribe(ctx, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-out:
	case <-time.After(5 * time.Second):
		t.Fatal("recognizer did not start")
	}
	cancel()
	done := make(chan struct{})
	go func() {
		for range out {
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("canceled recognizer did not close its output")
	}
}

func TestBrabbleDrainsOutputBeforeWaiting(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	engine := NewBrabbleEngine(BrabbleConfig{Command: executable, Args: []string{"-test.run=^TestBrabbleProcess$", "--", "brabble-output"}}, nil)
	out, err := engine.Transcribe(ctx, nil, Options{})
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for tr := range out {
		want := fmt.Sprintf("%03d %s", count, strings.Repeat("x", 100))
		if tr.Text != want {
			t.Fatalf("transcript %d = %q, want %q", count, tr.Text, want)
		}
		count++
		time.Sleep(time.Millisecond)
	}
	if count != 500 {
		t.Fatalf("delivered %d of 500 transcripts", count)
	}
}

func TestBrabbleDrainsStderr(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, logging := range []bool{false, true} {
		t.Run(fmt.Sprintf("logging=%v", logging), func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			var logf func(string, ...any)
			if logging {
				logf = func(string, ...any) {}
			}
			engine := NewBrabbleEngine(BrabbleConfig{Command: executable, Args: []string{"-test.run=^TestBrabbleProcess$", "--", "brabble-stderr"}}, logf)
			out, err := engine.Transcribe(ctx, nil, Options{})
			if err != nil {
				t.Fatal(err)
			}
			var transcripts []string
			for tr := range out {
				transcripts = append(transcripts, tr.Text)
			}
			if len(transcripts) != 1 || transcripts[0] != "complete" {
				t.Fatalf("stderr stalled stdout: %v (context: %v)", transcripts, ctx.Err())
			}
		})
	}
}
