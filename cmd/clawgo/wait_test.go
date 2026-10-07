package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"
)

func TestConnectBridgeCanceled(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	client, err := connectBridge(ctx, listener.Addr().String())
	if client != nil {
		client.Close()
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("connectBridge(canceled) = %v, want context.Canceled", err)
	}
}

func TestConnectBridgeCancellationClosesConnection(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client, err := connectBridge(ctx, listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	peer, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	cancel()
	select {
	case <-client.done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancellation did not close bridge")
	}
	if err := peer.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
		t.Fatal(err)
	}
	var buf [1]byte
	if _, err := peer.Read(buf[:]); err == nil {
		t.Fatal("peer read succeeded after cancellation")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("peer connection remained open after cancellation")
	}
}

func testBridgeClient() *BridgeClient {
	return &BridgeClient{
		logf:   func(string, ...any) {},
		frames: make(chan map[string]any),
	}
}

func TestWaitForPairCancelUnblocks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := testBridgeClient()
	done := make(chan error, 1)
	go func() {
		_, err := waitForPair(ctx, c)
		done <- err
	}()
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitForPair did not return after cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForPair canceled = %v, want context.Canceled", err)
	}
}

func TestWaitForHelloCancelUnblocks(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	c := testBridgeClient()
	done := make(chan error, 1)
	go func() {
		done <- waitForHello(ctx, c)
	}()
	cancel()

	var err error
	select {
	case err = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("waitForHello did not return after cancel")
	}
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("waitForHello canceled = %v, want context.Canceled", err)
	}
}

func TestWaitForPairReturnsTokenOnPairOK(t *testing.T) {
	c := testBridgeClient()
	c.frames = make(chan map[string]any, 1)
	c.frames <- map[string]any{"type": "pair-ok", "token": "tok-1"}

	token, err := waitForPair(context.Background(), c)
	if err != nil {
		t.Fatalf("waitForPair pair-ok: %v", err)
	}
	if token != "tok-1" {
		t.Fatalf("waitForPair token = %q, want tok-1", token)
	}
}

func TestWaitForHelloReturnsOnHelloOK(t *testing.T) {
	c := testBridgeClient()
	c.frames = make(chan map[string]any, 1)
	c.frames <- map[string]any{"type": "hello-ok", "serverName": "gw"}

	if err := waitForHello(context.Background(), c); err != nil {
		t.Fatalf("waitForHello hello-ok: %v", err)
	}
}
