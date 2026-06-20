package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"

	fwra "github.com/davidmarne/archistrator-platform/framework-go/resourceaccess"
)

// This file adds the TOOL-CALLING transport to the Ollama *Client via /api/chat —
// the multi-turn counterpart of the single-prompt Generate (/api/generate). It
// moves a running tool-calling conversation to Ollama's chat endpoint and returns
// the next assistant turn (content + tool_calls). The caller drives the loop. Like
// the Anthropic GenerateWithTools sibling, this transport owns no loop state and no
// tool semantics — it only moves bytes and maps faults.
//
// Ollama tool support varies by model/version; an unsupported model simply returns
// no tool_calls (the caller treats that as the model declining to call a tool).

// ChatTool is one tool the model may call. Parameters is a JSON Schema object
// describing the tool's arguments.
type ChatTool struct {
	Name        string
	Description string
	Parameters  json.RawMessage
}

// ChatToolCall is a model-emitted tool call, or a caller-supplied echo of a prior
// assistant tool call when replaying history. Arguments is a JSON object.
type ChatToolCall struct {
	Name      string
	Arguments json.RawMessage
}

// ChatMessage is one turn. Role is "user" | "assistant" | "tool". A "tool" message
// carries the result of a prior tool call in Content (Ollama correlates by order +
// optional ToolName, not by id).
type ChatMessage struct {
	Role      string
	Content   string
	ToolCalls []ChatToolCall
	ToolName  string
}

// ChatRequest is one blocking /api/chat turn.
type ChatRequest struct {
	Model    string
	Messages []ChatMessage
	Tools    []ChatTool
	Format   string
	Options  GenerateOptions
}

// ChatResponse is one assistant turn: free text and any tool calls the model wants
// executed, plus the raw token counters.
type ChatResponse struct {
	Content         string
	ToolCalls       []ChatToolCall
	Done            bool
	PromptEvalCount int
	EvalCount       int
}

// ---- wire shapes (Ollama /api/chat) ---------------------------------------

type ollamaChatFn struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments,omitempty"`
}

type ollamaChatToolCall struct {
	Function ollamaChatFn `json:"function"`
}

type ollamaChatMsg struct {
	Role      string               `json:"role"`
	Content   string               `json:"content"`
	ToolCalls []ollamaChatToolCall `json:"tool_calls,omitempty"`
	ToolName  string               `json:"tool_name,omitempty"`
}

type ollamaChatToolSchema struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type ollamaChatTool struct {
	Type     string               `json:"type"`
	Function ollamaChatToolSchema `json:"function"`
}

type ollamaChatReq struct {
	Model    string           `json:"model"`
	Messages []ollamaChatMsg  `json:"messages"`
	Tools    []ollamaChatTool `json:"tools,omitempty"`
	Stream   bool             `json:"stream"`
	Format   string           `json:"format,omitempty"`
	Options  GenerateOptions  `json:"options"`
}

type ollamaChatResp struct {
	Message         ollamaChatMsg `json:"message"`
	Done            bool          `json:"done"`
	PromptEvalCount int           `json:"prompt_eval_count"`
	EvalCount       int           `json:"eval_count"`
}

// Chat performs the blocking /api/chat call and maps transport/HTTP faults onto
// the framework error model exactly as Generate does.
func (c *Client) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	wire := ollamaChatReq{
		Model:   req.Model,
		Stream:  false,
		Format:  req.Format,
		Options: req.Options,
	}
	for _, m := range req.Messages {
		wm := ollamaChatMsg{Role: m.Role, Content: m.Content, ToolName: m.ToolName}
		for _, tc := range m.ToolCalls {
			wm.ToolCalls = append(wm.ToolCalls, ollamaChatToolCall{Function: ollamaChatFn{Name: tc.Name, Arguments: tc.Arguments}})
		}
		wire.Messages = append(wire.Messages, wm)
	}
	for _, t := range req.Tools {
		wire.Tools = append(wire.Tools, ollamaChatTool{
			Type:     "function",
			Function: ollamaChatToolSchema{Name: t.Name, Description: t.Description, Parameters: t.Parameters},
		})
	}

	body, err := json.Marshal(wire)
	if err != nil {
		return ChatResponse{}, fwra.Wrap(fwra.ContractMisuse, err, "llm.Chat: marshal request")
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return ChatResponse{}, fwra.Wrap(fwra.Infrastructure, err, "llm.Chat: build request")
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(httpReq)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return ChatResponse{}, fwra.Wrap(fwra.Transient, err, "llm.Chat: timed out / cancelled")
		}
		return ChatResponse{}, fwra.Wrap(fwra.Transient, err, "llm.Chat: provider unreachable")
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return ChatResponse{}, mapHTTPError(resp)
	}

	var out ollamaChatResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return ChatResponse{}, fwra.Wrap(fwra.Infrastructure, err, "llm.Chat: decode response")
	}

	res := ChatResponse{
		Content:         out.Message.Content,
		Done:            out.Done,
		PromptEvalCount: out.PromptEvalCount,
		EvalCount:       out.EvalCount,
	}
	for _, tc := range out.Message.ToolCalls {
		res.ToolCalls = append(res.ToolCalls, ChatToolCall{Name: tc.Function.Name, Arguments: tc.Function.Arguments})
	}
	return res, nil
}
