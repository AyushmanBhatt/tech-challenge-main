package assistant

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	"github.com/acai-travel/tech-challenge/internal/chat/model"
	"github.com/openai/openai-go/v2"
)

type Assistant struct {
	cli             openai.Client
	tools           []tool
	toolByName      map[string]tool
	toolDefinitions []openai.ChatCompletionToolUnionParam
}

func New() *Assistant {
	return NewWithClient(openai.NewClient())
}

func NewWithClient(cli openai.Client) *Assistant {
	tools, toolByName := newTools()
	definitions := make([]openai.ChatCompletionToolUnionParam, 0, len(tools))
	for _, t := range tools {
		definitions = append(definitions, openai.ChatCompletionFunctionTool(t.Definition()))
	}

	return &Assistant{
		cli:             cli,
		tools:           tools,
		toolByName:      toolByName,
		toolDefinitions: definitions,
	}
}

func (a *Assistant) Title(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "An empty conversation", nil
	}

	slog.InfoContext(ctx, "Generating title for conversation", "conversation_id", conv.ID)

	// For performance and to avoid the model answering the prompt, only send
	// a concise instruction plus the last user message. This encourages a
	// short summarizing title instead of an answer.
	var lastUser string
	for i := len(conv.Messages) - 1; i >= 0; i-- {
		if conv.Messages[i].Role == model.RoleUser {
			lastUser = conv.Messages[i].Content
			break
		}
	}
	if strings.TrimSpace(lastUser) == "" {
		// Fallback to the last message if no user message found (shouldn't happen)
		lastUser = conv.Messages[len(conv.Messages)-1].Content
	}

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a concise title generator. Create a short title that summarizes the user's message without answering it. The title should be a single line, no more than 80 characters, and should not include special characters, punctuation, or emojis."),
		openai.UserMessage(lastUser),
	}

	resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
		Model:    openai.ChatModelO1,
		Messages: msgs,
	})

	if err != nil {
		return "", err
	}

	if len(resp.Choices) == 0 || strings.TrimSpace(resp.Choices[0].Message.Content) == "" {
		return "", errors.New("empty response from OpenAI for title generation")
	}

	title := resp.Choices[0].Message.Content
	title = strings.ReplaceAll(title, "\n", " ")
	title = strings.Trim(title, " \t\r\n-\"'")

	if len(title) > 80 {
		title = title[:80]
	}

	return title, nil
}

func (a *Assistant) Reply(ctx context.Context, conv *model.Conversation) (string, error) {
	if len(conv.Messages) == 0 {
		return "", errors.New("conversation has no messages")
	}

	slog.InfoContext(ctx, "Generating reply for conversation", "conversation_id", conv.ID)

	msgs := []openai.ChatCompletionMessageParamUnion{
		openai.SystemMessage("You are a helpful, concise AI assistant. Provide accurate, safe, and clear responses."),
	}

	var collectedToolOutputs []string

	for _, m := range conv.Messages {
		switch m.Role {
		case model.RoleUser:
			msgs = append(msgs, openai.UserMessage(m.Content))
		case model.RoleAssistant:
			msgs = append(msgs, openai.AssistantMessage(m.Content))
		}
	}

	for i := 0; i < 15; i++ {
		resp, err := a.cli.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:    openai.ChatModelGPT4_1,
			Messages: msgs,
			Tools:    a.toolDefinitions,
		})

		if err != nil {
			return "", err
		}

		if len(resp.Choices) == 0 {
			return "", errors.New("no choices returned by OpenAI")
		}

		message := resp.Choices[0].Message
		if len(message.ToolCalls) > 0 {
			msgs = append(msgs, message.ToParam())

			for _, call := range message.ToolCalls {
				slog.InfoContext(ctx, "Tool call received", "name", call.Function.Name, "args", call.Function.Arguments)

				tool, ok := a.toolByName[call.Function.Name]
				if !ok {
					return "", errors.New("unknown tool call: " + call.Function.Name)
				}

				result := tool.Handle(ctx, call)
				msgs = append(msgs, result.Message)
				if result.Output != "" {
					collectedToolOutputs = append(collectedToolOutputs, result.Output)
				}
			}

			continue
		}

		final := message.Content
		if len(collectedToolOutputs) > 0 {
			final = "Tool Output:\n" + strings.Join(collectedToolOutputs, "\n\n") + "\n\n" + final
		}

		return final, nil
	}

	return "", errors.New("too many tool calls, unable to generate reply")
}
