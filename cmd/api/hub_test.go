package main

import (
	"errors"
	"testing"

	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
)

type fakeSender struct {
	last *v1.ChatStreamResponse
	fail bool
}

func (f *fakeSender) Send(r *v1.ChatStreamResponse) error {
	if f.fail {
		return errors.New("send fail")
	}
	f.last = r
	return nil
}

func TestConnectionHub_RegisterAndSend(t *testing.T) {
	hub := NewConnectionHub()

	senderA := &fakeSender{}
	senderB := &fakeSender{}

	idA := hub.Register("alice@example.com", senderA)
	_ = hub.Register("alice@example.com", senderB) // second connection

	resp := &v1.ChatStreamResponse{MsgId: "m1", FromEmail: "bob@example.com", Content: "hello"}

	if err := hub.SendToUser("alice@example.com", resp); err != nil {
		t.Fatalf("expected send success, got error: %v", err)
	}

	if senderA.last == nil || senderA.last.MsgId != "m1" {
		t.Fatalf("sender A did not receive message")
	}

	// Unregister senderA and ensure it no longer receives messages
	hub.Unregister("alice@example.com", idA)

	resp2 := &v1.ChatStreamResponse{MsgId: "m2", FromEmail: "charlie@example.com", Content: "yo"}
	if err := hub.SendToUser("alice@example.com", resp2); err != nil {
		t.Fatalf("expected send success after unregistering one connection: %v", err)
	}

	if senderA.last.MsgId == "m2" {
		t.Fatalf("sender A should not have received second message after unregister")
	}
}

func TestConnectionHub_SendToOffline(t *testing.T) {
	hub := NewConnectionHub()

	if err := hub.SendToUser("nobody@example.com", &v1.ChatStreamResponse{}); err == nil {
		t.Fatalf("expected error when sending to offline user")
	}
}

func TestConnectionHub_SendPartialFailure(t *testing.T) {
	hub := NewConnectionHub()

	ok := &fakeSender{}
	bad := &fakeSender{fail: true}

	_ = hub.Register("d@example.com", ok)
	_ = hub.Register("d@example.com", bad)

	if err := hub.SendToUser("d@example.com", &v1.ChatStreamResponse{MsgId: "x"}); err == nil {
		t.Fatalf("expected error due to partial sender failure")
	}

	// After a partial failure, the failing connection should have been
	// automatically unregistered. A subsequent send should succeed and only
	// reach the healthy sender.
	if err := hub.SendToUser("d@example.com", &v1.ChatStreamResponse{MsgId: "y"}); err != nil {
		t.Fatalf("expected send to succeed after cleanup of failed connections: %v", err)
	}

	if ok.last == nil || ok.last.MsgId != "y" {
		t.Fatalf("healthy sender did not receive message after cleanup")
	}
}
