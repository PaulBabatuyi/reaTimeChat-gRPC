package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/auth"
	"github.com/PaulBabatuyi/reaTimeChat-gRPC/internal/data"
	v1 "github.com/PaulBabatuyi/reaTimeChat-gRPC/proto/chat/v1"
	"google.golang.org/grpc/metadata"
)

// fakeUsers provides the subset of UsersStore used by ChatStream.
type fakeUsers struct {
	exists bool
}

func (f *fakeUsers) UserExists(ctx context.Context, email string) (bool, error) { return f.exists, nil }
func (f *fakeUsers) CreateUser(ctx context.Context, email, hashedPassword string) (*data.User, error) {
	return &data.User{Email: email}, nil
}
func (f *fakeUsers) GetUserByEmail(ctx context.Context, email string) (*data.User, error) {
	return &data.User{Email: email}, nil
}

// fakeMsgs provides the subset of MessagesStore used by ChatStream.
type fakeMsgs struct{}

func (f *fakeMsgs) SaveMessage(ctx context.Context, fromEmail, toEmail, content string, sentAt time.Time) (*data.Message, error) {
	return &data.Message{FromEmail: fromEmail, ToEmail: toEmail, Content: content, SentAt: sentAt}, nil
}
func (f *fakeMsgs) GetRecentChats(ctx context.Context, userEmail string, limit int64) ([]*data.ChatPartner, error) {
	return nil, nil
}
func (f *fakeMsgs) GetMessageHistory(ctx context.Context, user1, user2 string, limit int64) ([]*data.Message, error) {
	return nil, nil
}

// fakeStream implements the minimal subset of the bidirectional stream used by ChatStream.
type fakeStream struct {
	ctx context.Context
	// requests to return from Recv sequentially
	reqs []*v1.ChatStreamRequest
	// captured responses sent to this stream
	resp *v1.ChatStreamResponse
}

// badSender is an adapter around fakeStream which always returns an error on Send.
// We use it to emulate an unhealthy/broken connection registered in the hub.
type badSender struct{ *fakeStream }

func (b *badSender) Send(r *v1.ChatStreamResponse) error { return fmt.Errorf("broken") }

func (f *fakeStream) Recv() (*v1.ChatStreamRequest, error) {
	if len(f.reqs) == 0 {
		return nil, io.EOF
	}
	r := f.reqs[0]
	f.reqs = f.reqs[1:]
	return r, nil
}

func (f *fakeStream) Send(r *v1.ChatStreamResponse) error { f.resp = r; return nil }
func (f *fakeStream) Context() context.Context            { return f.ctx }

// The following methods are part of grpc.ServerStream; keep signatures exact so
// fakeStream implements the generated grpc.BidiStreamingServer interface.
func (f *fakeStream) SetHeader(md metadata.MD) error  { return nil }
func (f *fakeStream) SendHeader(md metadata.MD) error { return nil }
func (f *fakeStream) SetTrailer(md metadata.MD)       {}

// RecvMsg implements grpc.ServerStream RecvMsg to assist generic tests.
func (f *fakeStream) RecvMsg(m any) error {
	req, ok := m.(*v1.ChatStreamRequest)
	if !ok {
		return errors.New("RecvMsg: unexpected type")
	}
	r, err := f.Recv()
	if err != nil {
		return err
	}
	*req = *r
	return nil
}

// SendMsg implements grpc.ServerStream SendMsg to assist generic tests.
func (f *fakeStream) SendMsg(m any) error {
	resp, ok := m.(*v1.ChatStreamResponse)
	if !ok {
		return fmt.Errorf("SendMsg: unexpected type: %T", m)
	}
	return f.Send(resp)
}

func TestChatStream_DeliversToRecipient(t *testing.T) {
	// prepare hub and registers a fake recipient stream
	hub := NewConnectionHub()

	recipient := &fakeStream{ctx: context.WithValue(context.Background(), authContextKey{}, &auth.Claims{Email: "bob@example.com"})}
	// register recipient directly in hub so it's available when the sender sends
	_ = hub.Register("bob@example.com", recipient)

	// create server with fake dependencies and hub
	s := &Server{users: &fakeUsers{exists: true}, msgs: &fakeMsgs{}, auth: nil, hub: hub}

	// sender stream with one message destined to bob
	sender := &fakeStream{
		ctx:  context.WithValue(context.Background(), authContextKey{}, &auth.Claims{Email: "alice@example.com"}),
		reqs: []*v1.ChatStreamRequest{{ToEmail: "bob@example.com", Content: "hey bob"}},
	}

	// Run ChatStream (it will process one message and return when Recv EOF)
	if err := s.ChatStream(sender); err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}

	// sender should receive an acknowledgement with the message content
	if sender.resp == nil || sender.resp.Content != "hey bob" {
		t.Fatalf("sender did not receive ack or wrong content: %+v", sender.resp)
	}

	// recipient should have received a delivered message via hub
	if recipient.resp == nil || recipient.resp.Content != "hey bob" {
		t.Fatalf("recipient did not receive message via hub: %+v", recipient.resp)
	}
}

func TestChatStream_UnregistersOnEOF(t *testing.T) {
	// prepare hub and server with fake dependencies
	hub := NewConnectionHub()
	s := &Server{users: &fakeUsers{exists: true}, msgs: &fakeMsgs{}, auth: nil, hub: hub}

	// sender stream with no requests -> Recv returns EOF immediately
	sender := &fakeStream{ctx: context.WithValue(context.Background(), authContextKey{}, &auth.Claims{Email: "alice@example.com"}), reqs: []*v1.ChatStreamRequest{}}

	// Run ChatStream; it should register then immediately unregister on EOF.
	if err := s.ChatStream(sender); err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}

	// After stream returns, alice should no longer be registered/connected.
	if err := hub.SendToUser("alice@example.com", &v1.ChatStreamResponse{}); err == nil {
		t.Fatalf("expected error when sending to user who disconnected; got success")
	}
}

func TestChatStream_DeliversToMultipleRecipientConnectionsAndCleansFailed(t *testing.T) {
	hub := NewConnectionHub()

	// Two recipient connections: one healthy, one that fails to send
	recipientOK := &fakeStream{ctx: context.WithValue(context.Background(), authContextKey{}, &auth.Claims{Email: "bob@example.com"})}
	recipientBad := &fakeStream{ctx: context.WithValue(context.Background(), authContextKey{}, &auth.Claims{Email: "bob@example.com"})}

	// Make the bad recipient's Send return an error by wrapping it with a StreamSender
	// that returns an error — we'll register it using a small adapter.
	type badSender struct{ *fakeStream }
	func (b *badSender) Send(r *v1.ChatStreamResponse) error { return fmt.Errorf("broken") }

	// Register both recipients
	_ = hub.Register("bob@example.com", recipientOK)
	_ = hub.Register("bob@example.com", &badSender{recipientBad})

	// create server with fake dependencies and hub
	s := &Server{users: &fakeUsers{exists: true}, msgs: &fakeMsgs{}, auth: nil, hub: hub}

	// sender stream with one message destined to bob
	sender := &fakeStream{
		ctx:  context.WithValue(context.Background(), authContextKey{}, &auth.Claims{Email: "alice@example.com"}),
		reqs: []*v1.ChatStreamRequest{{ToEmail: "bob@example.com", Content: "hello all"}},
	}

	if err := s.ChatStream(sender); err != nil {
		t.Fatalf("ChatStream returned error: %v", err)
	}

	// Sender should receive ack
	if sender.resp == nil || sender.resp.Content != "hello all" {
		t.Fatalf("sender did not receive ack or wrong content: %+v", sender.resp)
	}

	// Healthy recipient should have received the message
	if recipientOK.resp == nil || recipientOK.resp.Content != "hello all" {
		t.Fatalf("healthy recipient did not get message: %+v", recipientOK.resp)
	}

	// After a failed delivery, the broken connection should be unregistered —
	// subsequent sends should only reach the healthy connection.
	if err := hub.SendToUser("bob@example.com", &v1.ChatStreamResponse{MsgId: "after"}); err != nil {
		t.Fatalf("expected send to succeed after cleanup, got err: %v", err)
	}
	if recipientOK.resp == nil || recipientOK.resp.MsgId != "after" {
		t.Fatalf("healthy recipient did not receive follow-up message: %+v", recipientOK.resp)
	}
}
