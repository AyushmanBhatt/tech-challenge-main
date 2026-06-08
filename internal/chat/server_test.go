package chat

import (
	"context"
	"testing"

	"github.com/acai-travel/tech-challenge/internal/chat/model"
	. "github.com/acai-travel/tech-challenge/internal/chat/testing"
	"github.com/acai-travel/tech-challenge/internal/pb"
	"github.com/google/go-cmp/cmp"
	"github.com/twitchtv/twirp"
	"google.golang.org/protobuf/testing/protocmp"
)

type mockAssistant struct {
	title string
	reply string
}

func (m *mockAssistant) Title(ctx context.Context, conv *model.Conversation) (string, error) {
	return m.title, nil
}

func (m *mockAssistant) Reply(ctx context.Context, conv *model.Conversation) (string, error) {
	return m.reply, nil
}

func TestServer_StartConversation(t *testing.T) {
	ctx := context.Background()
	mock := &mockAssistant{title: "Weather in Barcelona", reply: "It looks sunny in Barcelona."}
	srv := NewServer(model.New(ConnectMongo()), mock)

	WithFixture(func(t *testing.T, f *Fixture) {
		req := &pb.StartConversationRequest{Message: "What is the weather like in Barcelona?"}
		res, err := srv.StartConversation(ctx, req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		if res.GetTitle() != mock.title {
			t.Fatalf("expected title %q, got %q", mock.title, res.GetTitle())
		}

		if res.GetReply() != mock.reply {
			t.Fatalf("expected reply %q, got %q", mock.reply, res.GetReply())
		}

		conversation, err := srv.repo.DescribeConversation(ctx, res.GetConversationId())
		if err != nil {
			t.Fatalf("failed to fetch stored conversation: %v", err)
		}

		if conversation.Title != mock.title {
			t.Fatalf("expected stored title %q, got %q", mock.title, conversation.Title)
		}

		if len(conversation.Messages) != 2 {
			t.Fatalf("expected 2 messages stored, got %d", len(conversation.Messages))
		}
	})(t)
}

func TestServer_DescribeConversation(t *testing.T) {
	ctx := context.Background()
	srv := NewServer(model.New(ConnectMongo()), nil)

	t.Run("describe existing conversation", WithFixture(func(t *testing.T, f *Fixture) {
		c := f.CreateConversation()

		out, err := srv.DescribeConversation(ctx, &pb.DescribeConversationRequest{ConversationId: c.ID.Hex()})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		got, want := out.GetConversation(), c.Proto()
		if !cmp.Equal(got, want, protocmp.Transform()) {
			t.Errorf("DescribeConversation() mismatch (-got +want):\n%s", cmp.Diff(got, want, protocmp.Transform()))
		}
	}))

	t.Run("describe non existing conversation should return 404", WithFixture(func(t *testing.T, f *Fixture) {
		_, err := srv.DescribeConversation(ctx, &pb.DescribeConversationRequest{ConversationId: "08a59244257c872c5943e2a2"})
		if err == nil {
			t.Fatal("expected error for non-existing conversation, got nil")
		}

		if te, ok := err.(twirp.Error); !ok || te.Code() != twirp.NotFound {
			t.Fatalf("expected twirp.NotFound error, got %v", err)
		}
	}))
}
