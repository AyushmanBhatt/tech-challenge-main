package assistant

import (
	"context"

	"github.com/openai/openai-go/v2"
)

type tool struct {
	name        string
	description string
	parameters  openai.FunctionParameters
	handler     func(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) toolResult
}

type toolResult struct {
	Message openai.ChatCompletionMessageParamUnion
	Output  string
}

func newTool(name, description string, parameters openai.FunctionParameters, handler func(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) toolResult) tool {
	return tool{name: name, description: description, parameters: parameters, handler: handler}
}

func (t tool) Definition() openai.FunctionDefinitionParam {
	return openai.FunctionDefinitionParam{
		Name:        t.name,
		Description: openai.String(t.description),
		Parameters:  t.parameters,
	}
}

func (t tool) Handle(ctx context.Context, call openai.ChatCompletionMessageToolCallUnion) toolResult {
	return t.handler(ctx, call)
}
