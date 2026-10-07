package main

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"testing"
	"time"
)

func readLoopFixture(t *testing.T) (*BridgeClient, net.Conn, <-chan struct{}) {
	t.Helper()
	conn, peer := net.Pipe()
	c := &BridgeClient{
		conn:   conn,
		logf:   func(string, ...any) {},
		done:   make(chan struct{}),
		frames: make(chan map[string]any, 16),
	}
	finished := make(chan struct{})
	go func() {
		c.readLoop()
		close(finished)
	}()
	t.Cleanup(func() {
		c.Close()
		_ = peer.Close()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Error("bridge reader did not stop")
		}
	})
	return c, peer, finished
}

func TestBridgeHandshakeBeforeEOF(t *testing.T) {
	for _, kind := range []string{"pair", "hello"} {
		t.Run(kind, func(t *testing.T) {
			for i := 0; i < 32; i++ {
				c, peer, finished := readLoopFixture(t)
				frame := map[string]any{"type": kind + "-ok", "token": "synthetic-token"}
				if err := json.NewEncoder(peer).Encode(frame); err != nil {
					t.Fatal(err)
				}
				_ = peer.Close()
				select {
				case <-finished:
				case <-time.After(2 * time.Second):
					t.Fatal("bridge reader did not finish after EOF")
				}
				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				if kind == "pair" {
					token, err := waitForPair(ctx, c)
					cancel()
					if err != nil || token != "synthetic-token" {
						t.Fatalf("iteration %d: pair = %q, %v; want token before EOF", i, token, err)
					}
				} else {
					err := waitForHello(ctx, c)
					cancel()
					if err != nil {
						t.Fatalf("iteration %d: hello = %v; want hello-ok before EOF", i, err)
					}
				}
			}
		})
	}
}

func TestBridgeReaderStopsWithoutConsumer(t *testing.T) {
	for _, frames := range []int{0, 17} {
		c, peer, finished := readLoopFixture(t)
		writerDone := make(chan struct{})
		go func() {
			defer close(writerDone)
			for i := 0; i < frames; i++ {
				if _, err := io.WriteString(peer, "{\"type\":\"event\"}\n"); err != nil {
					return
				}
			}
		}()
		select {
		case <-writerDone:
		case <-time.After(2 * time.Second):
			t.Fatal("could not fill frame buffer")
		}
		c.Close()
		select {
		case <-finished:
		case <-time.After(2 * time.Second):
			t.Fatal("reader blocked after consumer left")
		}
	}
}

func TestBridgeWaitReportsEOF(t *testing.T) {
	c, peer, _ := readLoopFixture(t)
	_ = peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := waitForPair(ctx, c); err != io.EOF {
		t.Fatalf("pair error = %v, want EOF", err)
	}
	if err := waitForHello(ctx, c); err != io.EOF {
		t.Fatalf("hello error = %v, want EOF", err)
	}
}

type failedReadConn struct {
	net.Conn
	err error
}

func (c failedReadConn) Read([]byte) (int, error) { return 0, c.err }

func TestBridgeReaderPreservesReadError(t *testing.T) {
	want := io.ErrUnexpectedEOF
	c := &BridgeClient{
		conn:   failedReadConn{err: want},
		frames: make(chan map[string]any, 1),
	}
	c.readLoop()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := waitForPair(ctx, c); err != want {
		t.Fatalf("pair error = %v, want %v", err, want)
	}
	if err := waitForHello(ctx, c); err != want {
		t.Fatalf("hello error = %v, want %v", err, want)
	}
}
