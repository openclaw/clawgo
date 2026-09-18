package main

import (
	"errors"
	"net"
	"testing"
	"time"
)

func testReadLoopClient(t *testing.T) (*BridgeClient, net.Conn) {
	t.Helper()
	clientConn, serverConn := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
	})
	c := &BridgeClient{
		conn:   clientConn,
		logf:   func(string, ...any) {},
		done:   make(chan struct{}),
		errs:   make(chan error, 1),
		frames: make(chan map[string]any, 16),
	}
	return c, serverConn
}

func TestReadLoopDoesNotHangWhenErrorConsumerLeaves(t *testing.T) {
	c, serverConn := testReadLoopClient(t)

	// Production errs is buffer 1. After handleFrame fails, runNode
	// Close()s and stops selecting on this client. An unread error
	// already occupies the slot, so the trailing send would block.
	c.errs <- errors.New("unread")

	finished := make(chan struct{})
	go func() {
		c.readLoop()
		close(finished)
	}()

	_ = serverConn.Close()
	c.Close()

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop blocked sending on errs after consumer left")
	}
}

func TestReadLoopTerminatesWhenProductionErrorBufferIsFull(t *testing.T) {
	c, serverConn := testReadLoopClient(t)

	firstDone := make(chan struct{})
	go func() {
		c.readLoop()
		close(firstDone)
	}()
	_ = serverConn.Close()
	select {
	case <-firstDone:
	case <-time.After(2 * time.Second):
		t.Fatal("first production readLoop did not return after server close")
	}
	select {
	case c.errs <- errors.New("probe"):
		t.Fatal("errs slot was empty; production sendErr did not fill it")
	default:
	}

	clientConn2, serverConn2 := net.Pipe()
	t.Cleanup(func() {
		_ = clientConn2.Close()
		_ = serverConn2.Close()
	})
	c.conn = clientConn2

	secondDone := make(chan struct{})
	started := time.Now()
	go func() {
		c.readLoop()
		close(secondDone)
	}()
	_ = serverConn2.Close()
	c.Close()
	select {
	case <-secondDone:
		t.Logf("second production readLoop returned after Close with full errs in %s", time.Since(started))
	case <-time.After(2 * time.Second):
		t.Fatal("second production readLoop blocked on full errs after Close")
	}
}

func TestReadLoopPublishesErrorWhenConsumerIsPresent(t *testing.T) {
	c, serverConn := testReadLoopClient(t)

	finished := make(chan struct{})
	go func() {
		c.readLoop()
		close(finished)
	}()

	_ = serverConn.Close()

	var err error
	select {
	case err = <-c.errs:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not publish an error to a live consumer")
	}
	if err == nil {
		t.Fatal("readLoop published nil error")
	}

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("readLoop did not return after publishing")
	}
}
