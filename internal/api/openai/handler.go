package openai

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/irisvn/kiro-let-go/internal/kiro"
	"github.com/irisvn/kiro-let-go/internal/normalized"
)

const clientClosedRequestStatus = 499

const (
	requestLogKeyModel        = "rl_model"
	requestLogKeyInputTokens  = "rl_input_tokens"
	requestLogKeyOutputTokens = "rl_output_tokens"
	requestLogKeyStream       = "rl_stream"
)

type chatDispatcher interface {
	Stream(context.Context, *kiro.KiroPayload, account.SelectionHint) (<-chan kiro.StreamEvent, error)
	Once(context.Context, *kiro.KiroPayload, account.SelectionHint) (kiro.FullResponse, error)
}

// HandlerOptions configures the OpenAI chat completions handler.
type HandlerOptions struct {
	Dispatcher chatDispatcher
	Now        func() time.Time
	NewID      func() string
}

// Handler returns a POST /v1/chat/completions-compatible gin handler.
func Handler(opts HandlerOptions) gin.HandlerFunc {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.NewID == nil {
		opts.NewID = uuid.NewString
	}

	return func(c *gin.Context) {
		var req ChatCompletionRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			writeJSONError(c, invalidRequestError(err.Error()))
			return
		}
		if err := req.Validate(); err != nil {
			writeJSONError(c, invalidRequestError(err.Error()))
			return
		}
		c.Set(requestLogKeyModel, req.Model)
		c.Set(requestLogKeyStream, req.Stream)

		normalizedReq, err := OpenAIToNormalized(&req)
		if err != nil {
			writeJSONError(c, requestError(err))
			return
		}

		payload, err := normalized.NormalizedToKiro(normalizedReq, "")
		if err != nil {
			writeJSONError(c, requestError(err))
			return
		}

		hint := account.SelectionHint{
			ConversationID: payload.ConversationState.ConversationID,
			Model:          payload.ConversationState.CurrentMessage.UserInputMessage.ModelID,
		}

		responseID := "chatcmpl-" + opts.NewID()
		created := opts.Now().Unix()
		if req.Stream {
			handleStream(c, opts.Dispatcher, &req, payload, hint, responseID, created, wantsReasoning(&req, normalizedReq))
			return
		}

		handleOnce(c, opts.Dispatcher, &req, payload, hint, responseID, created, wantsReasoning(&req, normalizedReq))
	}
}

func handleStream(c *gin.Context, dispatcher chatDispatcher, req *ChatCompletionRequest, payload *kiro.KiroPayload, hint account.SelectionHint, responseID string, created int64, includeReasoning bool) {
	setStreamingHeaders(c)
	writer := NewSSEWriter(c.Writer, c.Writer, responseID, req.Model, created)

	if dispatcher == nil {
		writer.WriteError(classifyHandlerError(errs.New(errs.ClassFatal, "DISPATCHER_NOT_READY", "dispatcher is not configured")))
		return
	}

	events, err := dispatcher.Stream(c.Request.Context(), payload, hint)
	if err != nil {
		writer.WriteError(classifyHandlerError(err))
		return
	}

	writer.writeChunk(ChatCompletionChunk{
		Choices: []ChunkChoice{{Index: 0, Delta: Delta{Role: "assistant"}}},
	})

	var usage *Usage
	for event := range events {
		switch e := event.(type) {
		case kiro.TextDelta:
			writer.WriteContentDelta(e.Text)
		case kiro.ThinkingDelta:
			if includeReasoning {
				writer.WriteReasoningDelta(e.Text)
			}
		case kiro.ToolUseStart:
			writer.WriteToolCallStart(e.ID, e.Name)
		case kiro.ToolUseDelta:
			writer.WriteToolCallArgsDelta(e.ID, e.InputDelta)
		case kiro.ToolUseStop:
			writer.WriteToolCallStop(e.ID)
		case kiro.Usage:
			usage = usageFromKiroPtr(e)
			c.Set(requestLogKeyInputTokens, e.InputTokens)
			c.Set(requestLogKeyOutputTokens, e.OutputTokens)
		case kiro.Stop:
			writer.WriteFinalChunk(mapFinishReason(e.Reason), usage)
			writer.WriteDone()
			return
		case kiro.ErrorEvent:
			writer.WriteError(classifyHandlerError(e.Err))
			return
		}
	}

	writer.WriteFinalChunk("stop", usage)
	writer.WriteDone()
}

func handleOnce(c *gin.Context, dispatcher chatDispatcher, req *ChatCompletionRequest, payload *kiro.KiroPayload, hint account.SelectionHint, responseID string, created int64, includeReasoning bool) {
	if dispatcher == nil {
		writeJSONError(c, classifyHandlerError(errs.New(errs.ClassFatal, "DISPATCHER_NOT_READY", "dispatcher is not configured")))
		return
	}

	full, err := dispatcher.Once(c.Request.Context(), payload, hint)
	if err != nil {
		writeJSONError(c, classifyHandlerError(err))
		return
	}
	c.Set(requestLogKeyInputTokens, full.Usage.InputTokens)
	c.Set(requestLogKeyOutputTokens, full.Usage.OutputTokens)

	message := ChatMessage{
		Role:      "assistant",
		Content:   MessageContent{Text: full.Text},
		ToolCalls: make([]ToolCall, 0, len(full.ToolUses)),
	}
	if includeReasoning && full.Thinking != "" {
		message.ReasoningContent = full.Thinking
	}
	for _, toolUse := range full.ToolUses {
		message.ToolCalls = append(message.ToolCalls, ToolCall{
			ID:   toolUse.ToolUseID,
			Type: "function",
			Function: ToolCallFunction{
				Name:      toolUse.Name,
				Arguments: toolUse.Input,
			},
		})
	}

	c.JSON(http.StatusOK, ChatCompletionResponse{
		ID:      responseID,
		Object:  "chat.completion",
		Created: created,
		Model:   req.Model,
		Choices: []Choice{{
			Index:        0,
			Message:      message,
			FinishReason: mapFinishReason(full.StopReason),
		}},
		Usage: usageFromKiro(full.Usage),
	})
}

func setStreamingHeaders(c *gin.Context) {
	header := c.Writer.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
}

func wantsReasoning(req *ChatCompletionRequest, normalizedReq *normalized.NormalizedRequest) bool {
	if req != nil && req.ReasoningEffort != "" {
		return true
	}
	return normalizedReq != nil && normalizedReq.ThinkingBudget > 0
}

func mapFinishReason(reason string) string {
	switch reason {
	case "tool_use":
		return "tool_calls"
	case "max_tokens":
		return "length"
	case "", "end_turn":
		return "stop"
	default:
		return "stop"
	}
}

func usageFromKiro(usage kiro.Usage) Usage {
	return Usage{
		PromptTokens:     usage.InputTokens,
		CompletionTokens: usage.OutputTokens,
		TotalTokens:      usage.InputTokens + usage.OutputTokens,
	}
}

func usageFromKiroPtr(usage kiro.Usage) *Usage {
	u := usageFromKiro(usage)
	return &u
}

func invalidRequestError(message string) *errs.Error {
	return &errs.Error{Class: errs.ClassFatal, Code: "invalid_request_error", HTTPStatus: http.StatusBadRequest, Message: message}
}

func requestError(err error) *errs.Error {
	if err == nil {
		return invalidRequestError("invalid request")
	}
	var classified *errs.Error
	if errors.As(err, &classified) && classified != nil {
		out := *classified
		if out.Message == "" {
			out.Message = err.Error()
		}
		if out.Code == "" {
			out.Code = "invalid_request_error"
		}
		out.HTTPStatus = http.StatusBadRequest
		return &out
	}
	return invalidRequestError(err.Error())
}

func classifyHandlerError(err error) *errs.Error {
	if err == nil {
		return &errs.Error{Class: errs.ClassFatal, Code: "internal_error", HTTPStatus: http.StatusInternalServerError, Message: "internal error"}
	}

	var classified *errs.Error
	if !errors.As(err, &classified) || classified == nil {
		return &errs.Error{Class: errs.ClassFatal, Code: "internal_error", HTTPStatus: http.StatusInternalServerError, Message: err.Error(), Cause: err}
	}

	out := *classified
	if out.Message == "" {
		out.Message = http.StatusText(httpStatusForError(&out))
	}
	if out.Code == "" {
		out.Code = out.Class.String()
	}
	if out.HTTPStatus == 0 {
		out.HTTPStatus = httpStatusForError(&out)
	}
	return &out
}

func writeJSONError(c *gin.Context, err *errs.Error) {
	classified := classifyHandlerError(err)
	c.AbortWithStatusJSON(classified.HTTPStatus, struct {
		Error openAIError `json:"error"`
	}{Error: openAIError{
		Message: classified.Message,
		Type:    classified.Code,
		Code:    classified.Code,
		Status:  classified.HTTPStatus,
	}})
}

func httpStatusForError(err *errs.Error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	if err.HTTPStatus != 0 {
		return err.HTTPStatus
	}
	switch err.Class {
	case errs.ClassQuotaExhausted:
		return http.StatusPaymentRequired
	case errs.ClassAuthExpired:
		return http.StatusUnauthorized
	case errs.ClassRateLimited:
		return http.StatusTooManyRequests
	case errs.ClassContentTooLong:
		return http.StatusBadRequest
	case errs.ClassRecoverable, errs.ClassNetwork:
		return http.StatusServiceUnavailable
	case errs.ClassClientCanceled:
		return clientClosedRequestStatus
	case errs.ClassFatal:
		return http.StatusBadGateway
	default:
		return http.StatusInternalServerError
	}
}
