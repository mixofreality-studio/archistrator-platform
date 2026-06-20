package llm

import (
	"context"
	"encoding/json"

	"github.com/anthropics/anthropic-sdk-go"
	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// This file adds the TOOL-CALLING transport to AnthropicClient — the multi-turn
// counterpart of the single-text Generate in anthropic.go. It moves a running
// tool-calling conversation (system + messages + tool defs) to the Messages API
// and returns the next assistant turn (text + tool_use blocks + stop reason). The
// caller (a WorkerAccess) drives the loop: it executes each tool_use, appends a
// tool_result, and calls GenerateWithTools again. This transport owns NO loop
// state and NO tool semantics — it only maps bytes to/from the provider.

// AnthropicTool is one tool the model may call. InputSchema is a JSON Schema
// object (draft 2020-12) describing the tool's `input`. Strict requests
// constrained decoding so the emitted input is guaranteed schema-valid.
type AnthropicTool struct {
	Name        string
	Description string
	InputSchema json.RawMessage
	Strict      bool
}

// AnthropicToolUse is a model-emitted tool call (a tool_use content block) or a
// caller-supplied echo of a prior assistant tool call when replaying history.
type AnthropicToolUse struct {
	ID    string
	Name  string
	Input json.RawMessage
}

// AnthropicToolResult is the caller's answer to a prior AnthropicToolUse, carried
// on a user turn as a tool_result block. Content is the result text (an ok marker
// or, on IsError, the actionable failure the model should self-correct from).
type AnthropicToolResult struct {
	ToolUseID string
	Content   string
	IsError   bool
}

// AnthropicMessage is one turn of the running conversation. Role is "user" or
// "assistant". A user turn carries Text and/or ToolResults; an assistant turn
// carries Text and/or ToolUses (the model's prior tool calls, echoed back so the
// provider sees the full history).
type AnthropicMessage struct {
	Role        string
	Text        string
	ToolUses    []AnthropicToolUse
	ToolResults []AnthropicToolResult
}

// AnthropicToolRequest is one blocking tool-calling turn. Messages is the full
// running conversation (the caller grows it across turns); Tools is the permitted
// tool set. System is an optional provider-mechanical instruction.
type AnthropicToolRequest struct {
	Model     string
	System    string
	MaxTokens int
	Messages  []AnthropicMessage
	Tools     []AnthropicTool
}

// AnthropicToolResponse is one assistant turn: any free text, the tool calls the
// model wants executed, the stop reason ("tool_use" when it wants tools,
// "end_turn" when it is done), and the raw token counters.
type AnthropicToolResponse struct {
	Text         string
	ToolUses     []AnthropicToolUse
	StopReason   string
	InputTokens  int
	OutputTokens int
}

// GenerateWithTools performs one blocking tool-calling Messages-API turn. Faults
// are mapped onto the framework error model by HTTP status code exactly as
// Generate does (mapAnthropicError).
func (c *AnthropicClient) GenerateWithTools(ctx context.Context, req AnthropicToolRequest) (AnthropicToolResponse, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.maxTokens
	}

	tools := make([]anthropic.ToolUnionParam, 0, len(req.Tools))
	for _, t := range req.Tools {
		var schema anthropic.ToolInputSchemaParam
		if len(t.InputSchema) > 0 {
			if err := json.Unmarshal(t.InputSchema, &schema); err != nil {
				return AnthropicToolResponse{}, fwra.Wrap(fwra.ContractMisuse, err, "anthropic.GenerateWithTools: tool input schema")
			}
		}
		tp := anthropic.ToolParam{Name: t.Name, InputSchema: schema}
		if t.Description != "" {
			tp.Description = anthropic.String(t.Description)
		}
		if t.Strict {
			tp.Strict = anthropic.Bool(true)
		}
		tools = append(tools, anthropic.ToolUnionParam{OfTool: &tp})
	}

	msgs := make([]anthropic.MessageParam, 0, len(req.Messages))
	for _, m := range req.Messages {
		var blocks []anthropic.ContentBlockParamUnion
		if m.Text != "" {
			blocks = append(blocks, anthropic.NewTextBlock(m.Text))
		}
		for _, tu := range m.ToolUses {
			var input any = json.RawMessage(tu.Input)
			blocks = append(blocks, anthropic.NewToolUseBlock(tu.ID, input, tu.Name))
		}
		for _, tr := range m.ToolResults {
			blocks = append(blocks, anthropic.NewToolResultBlock(tr.ToolUseID, tr.Content, tr.IsError))
		}
		if m.Role == "assistant" {
			msgs = append(msgs, anthropic.NewAssistantMessage(blocks...))
		} else {
			msgs = append(msgs, anthropic.NewUserMessage(blocks...))
		}
	}

	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: int64(maxTokens),
		Messages:  msgs,
		Tools:     tools,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}

	msg, err := c.client.Messages.New(ctx, params)
	if err != nil {
		return AnthropicToolResponse{}, mapAnthropicError(ctx, err)
	}

	out := AnthropicToolResponse{
		StopReason:   string(msg.StopReason),
		InputTokens:  int(msg.Usage.InputTokens),
		OutputTokens: int(msg.Usage.OutputTokens),
	}
	for _, block := range msg.Content {
		switch v := block.AsAny().(type) {
		case anthropic.TextBlock:
			out.Text += v.Text
		case anthropic.ToolUseBlock:
			out.ToolUses = append(out.ToolUses, AnthropicToolUse{ID: v.ID, Name: v.Name, Input: v.Input})
		}
	}
	return out, nil
}
