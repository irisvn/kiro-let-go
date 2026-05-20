package anthropic

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
	"github.com/irisvn/kiro-let-go/internal/kiro"
)

const anthropicPingInterval = 25 * time.Second

type dispatcherStreamFunc func(*kiro.Dispatcher, context.Context, *kiro.KiroPayload, account.SelectionHint) (<-chan kiro.StreamEvent, error)
type dispatcherOnceFunc func(*kiro.Dispatcher, context.Context, *kiro.KiroPayload, account.SelectionHint) (kiro.FullResponse, error)
type idFunc func() string

func defaultDispatcherStream(d *kiro.Dispatcher, ctx context.Context, payload *kiro.KiroPayload, hint account.SelectionHint) (<-chan kiro.StreamEvent, error) {
	return d.Stream(ctx, payload, hint)
}

func defaultDispatcherOnce(d *kiro.Dispatcher, ctx context.Context, payload *kiro.KiroPayload, hint account.SelectionHint) (kiro.FullResponse, error) {
	return d.Once(ctx, payload, hint)
}

func defaultNewMessageID() string {
	return "msg_" + uuid.NewString()
}

type Handler struct {
	dispatcher       *kiro.Dispatcher
	tokenizer        *kiro.Estimator
	logger           *slog.Logger
	dispatcherStream dispatcherStreamFunc
	dispatcherOnce   dispatcherOnceFunc
	newMessageID     idFunc
	pingInterval     time.Duration
}

func NewHandler(dispatcher *kiro.Dispatcher, tokenizer *kiro.Estimator, logger *slog.Logger) *Handler {
	if tokenizer == nil {
		tokenizer = &kiro.Estimator{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{
		dispatcher:       dispatcher,
		tokenizer:        tokenizer,
		logger:           logger,
		dispatcherStream: defaultDispatcherStream,
		dispatcherOnce:   defaultDispatcherOnce,
		newMessageID:     defaultNewMessageID,
		pingInterval:     anthropicPingInterval,
	}
}

func (h *Handler) Register(r gin.IRouter) {
	r.POST("/v1/messages", h.PostMessages)
	r.POST("/v1/messages/count_tokens", h.PostCountTokens)
}

func (h *Handler) PostMessages(c *gin.Context) {
	if h == nil || h.dispatcher == nil {
		writeJSONError(c, http.StatusInternalServerError, errs.New(errs.ClassFatal, "DISPATCHER_NOT_READY", "dispatcher is not configured"))
		return
	}

	var req MessagesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}

	normalized, err := anthropicToNormalized(&req)
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}
	payload, err := normalizedToKiro(normalized, "")
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}

	inputTokens, err := h.estimateInputTokens(payload)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, errs.Wrap(err, errs.ClassFatal, "failed to estimate input tokens"))
		return
	}

	hint := account.SelectionHint{ConversationID: payload.ConversationState.ConversationID, Model: req.Model}
	if req.Stream {
		h.stream(c, &req, payload, hint, inputTokens)
		return
	}
	h.once(c, &req, payload, hint, inputTokens)
}

func (h *Handler) PostCountTokens(c *gin.Context) {
	var req CountTokensRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}
	if err := req.Validate(); err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}

	normalized, err := countTokensToNormalized(&req)
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}
	payload, err := normalizedToKiro(normalized, "")
	if err != nil {
		writeJSONError(c, http.StatusBadRequest, errs.New(errs.ClassFatal, "INVALID_REQUEST", err.Error()))
		return
	}

	inputTokens, err := h.estimateInputTokens(payload)
	if err != nil {
		writeJSONError(c, http.StatusInternalServerError, errs.Wrap(err, errs.ClassFatal, "failed to estimate input tokens"))
		return
	}

	c.JSON(http.StatusOK, CountTokensResponse{InputTokens: inputTokens})
}

func (h *Handler) stream(c *gin.Context, req *MessagesRequest, payload *kiro.KiroPayload, hint account.SelectionHint, inputTokens int) {
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		writeJSONError(c, http.StatusInternalServerError, errs.New(errs.ClassFatal, "STREAM_UNSUPPORTED", "response writer does not support streaming"))
		return
	}

	setSSEHeaders(c)
	flusher.Flush()

	writer := NewSSEWriter(c.Writer, h.newMessageID(), req.Model)
	if err := writer.WriteMessageStart(inputTokens); err != nil {
		h.logWriteError("message_start", err)
		return
	}

	events, err := h.dispatcherStream(h.dispatcher, c.Request.Context(), payload, hint)
	if err != nil {
		h.writeStreamError(c.Request.Context(), writer, err)
		return
	}

	state := newStreamState(writer)
	done := make(chan struct{})
	activity := make(chan struct{}, 1)
	pingDone := make(chan struct{})
	go h.runPingLoop(c.Request.Context(), writer, activity, done, pingDone)
	markStreamActivity(activity)
	defer func() {
		close(done)
		<-pingDone
	}()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				return
			}
			markStreamActivity(activity)
			stop, err := state.handle(event)
			if err != nil {
				if _, ok := event.(kiro.ErrorEvent); ok {
					h.writeStreamError(c.Request.Context(), writer, err)
					return
				}
				if c.Request.Context().Err() == nil {
					h.logger.Error("failed to write anthropic stream event", "error", err)
				}
				return
			}
			if stop {
				return
			}
		}
	}
}

func (h *Handler) once(c *gin.Context, req *MessagesRequest, payload *kiro.KiroPayload, hint account.SelectionHint, inputTokens int) {
	full, err := h.dispatcherOnce(h.dispatcher, c.Request.Context(), payload, hint)
	if err != nil {
		classified := classifyHandlerError(err)
		if errors.Is(classified, context.Canceled) || errs.Is(classified, errs.ClassClientCanceled) {
			c.Status(statusClientClosedRequest)
			return
		}
		writeJSONError(c, statusForError(classified), classified)
		return
	}

	resp := MessagesResponse{
		ID:      h.newMessageID(),
		Type:    "message",
		Role:    "assistant",
		Content: buildResponseContent(full),
		Model:   req.Model,
		Usage: Usage{
			InputTokens:  inputTokens,
			OutputTokens: full.Usage.OutputTokens,
		},
	}
	stopReason := normalizeStopReason(full.StopReason, len(full.ToolUses) > 0)
	resp.StopReason = &stopReason
	body, err := json.Marshal(resp)
	if err != nil {
		if h != nil && h.logger != nil {
			h.logger.Error("failed to marshal anthropic response", "error", err)
		}
		writeJSONError(c, http.StatusInternalServerError, errs.Wrap(err, errs.ClassFatal, "failed to marshal anthropic response"))
		return
	}
	c.Data(http.StatusOK, "application/json; charset=utf-8", body)
}

func (h *Handler) estimateInputTokens(payload *kiro.KiroPayload) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	if h.tokenizer == nil {
		h.tokenizer = &kiro.Estimator{}
	}
	return h.tokenizer.CountTokens(string(body)), nil
}

func (h *Handler) runPingLoop(ctx context.Context, writer *SSEWriter, activity <-chan struct{}, done <-chan struct{}, pingDone chan<- struct{}) {
	defer close(pingDone)
	if h.pingInterval <= 0 {
		return
	}
	timer := time.NewTimer(h.pingInterval)
	defer timer.Stop()

	for {
		select {
		case <-done:
			return
		case <-ctx.Done():
			return
		case <-activity:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			timer.Reset(h.pingInterval)
		case <-timer.C:
			if err := writer.WritePing(); err != nil {
				h.logWriteError("ping", err)
				return
			}
			timer.Reset(h.pingInterval)
		}
	}
}

func (h *Handler) writeStreamError(ctx context.Context, writer *SSEWriter, err error) {
	if ctx.Err() != nil {
		return
	}
	classified := classifyHandlerError(err)
	if writeErr := writer.WriteError(classified); writeErr != nil {
		h.logWriteError("error", writeErr)
	}
}

func (h *Handler) logWriteError(event string, err error) {
	if h == nil || h.logger == nil || err == nil {
		return
	}
	if errs.Is(err, errs.ClassClientCanceled) || errors.Is(err, context.Canceled) || strings.Contains(err.Error(), "broken pipe") {
		return
	}
	h.logger.Error("failed to write anthropic SSE event", "event", event, "error", err)
}

type streamState struct {
	writer         *SSEWriter
	thinkingOpen   bool
	textIndex      int
	nextIndex      int
	toolIndices    map[string]int
	toolBlocksSeen bool
	openToolIDs    map[string]struct{}
}

func newStreamState(writer *SSEWriter) *streamState {
	return &streamState{
		writer:      writer,
		textIndex:   -1,
		nextIndex:   1,
		toolIndices: make(map[string]int),
		openToolIDs: make(map[string]struct{}),
	}
}

func (s *streamState) handle(event kiro.StreamEvent) (bool, error) {
	switch e := event.(type) {
	case kiro.ThinkingDelta:
		if !s.thinkingOpen {
			if err := s.writer.WriteContentBlockStart(0, "thinking", "", ""); err != nil {
				return false, err
			}
			s.thinkingOpen = true
		}
		return false, s.writer.WriteContentBlockDelta(0, ThinkingDelta{Type: "thinking_delta", Thinking: e.Text})
	case kiro.TextDelta:
		if s.textIndex == -1 {
			s.textIndex = s.allocateIndex()
			if err := s.writer.WriteContentBlockStart(s.textIndex, "text", "", ""); err != nil {
				return false, err
			}
		}
		return false, s.writer.WriteContentBlockDelta(s.textIndex, TextDelta{Type: "text_delta", Text: e.Text})
	case kiro.ToolUseStart:
		idx := s.toolIndex(e.ID)
		if err := s.writer.WriteContentBlockStart(idx, "tool_use", e.ID, e.Name); err != nil {
			return false, err
		}
		s.toolBlocksSeen = true
		s.openToolIDs[e.ID] = struct{}{}
		return false, nil
	case kiro.ToolUseDelta:
		idx := s.toolIndex(e.ID)
		if _, ok := s.openToolIDs[e.ID]; !ok {
			if err := s.writer.WriteContentBlockStart(idx, "tool_use", e.ID, ""); err != nil {
				return false, err
			}
			s.toolBlocksSeen = true
			s.openToolIDs[e.ID] = struct{}{}
		}
		return false, s.writer.WriteContentBlockDelta(idx, InputJSONDelta{Type: "input_json_delta", PartialJSON: e.InputDelta})
	case kiro.ToolUseStop:
		idx, ok := s.toolIndices[e.ID]
		if !ok {
			return false, nil
		}
		delete(s.openToolIDs, e.ID)
		return false, s.writer.WriteContentBlockStop(idx)
	case kiro.Stop:
		if err := s.closeAll(); err != nil {
			return false, err
		}
		stopReason := normalizeStopReason(e.Reason, s.toolBlocksSeen)
		if err := s.writer.WriteMessageDelta(stopReason); err != nil {
			return false, err
		}
		return true, s.writer.WriteMessageStop()
	case kiro.ErrorEvent:
		return false, e.Err
	default:
		return false, nil
	}
}

func (s *streamState) allocateIndex() int {
	idx := s.nextIndex
	s.nextIndex++
	return idx
}

func (s *streamState) toolIndex(id string) int {
	if idx, ok := s.toolIndices[id]; ok {
		return idx
	}
	idx := s.allocateIndex()
	s.toolIndices[id] = idx
	return idx
}

func (s *streamState) closeAll() error {
	open := make([]int, 0, 2+len(s.openToolIDs))
	if s.thinkingOpen {
		open = append(open, 0)
		if s.textIndex != -1 {
			open = append(open, s.textIndex)
		}
	} else if s.textIndex != -1 {
		open = append(open, s.textIndex)
	}
	for id := range s.openToolIDs {
		open = append(open, s.toolIndices[id])
	}
	sort.Ints(open)
	for _, idx := range open {
		if err := s.writer.WriteContentBlockStop(idx); err != nil {
			return err
		}
	}
	s.thinkingOpen = false
	s.textIndex = -1
	clear(s.openToolIDs)
	return nil
}

type handlerNormalizedRequest struct {
	Model           string
	SystemPrompt    string
	Messages        []handlerNormalizedMessage
	Tools           []handlerNormalizedTool
	ToolChoice      handlerNormalizedToolChoice
	MaxOutputTokens int
	Temperature     float64
	TopP            float64
	TopK            int
	Stop            []string
	ThinkingBudget  int
	Stream          bool
}

type handlerNormalizedMessage struct {
	Role  string
	Parts []handlerNormalizedPart
}

type handlerNormalizedPart any

type handlerText struct{ Text string }
type handlerImage struct {
	MediaType string
	DataB64   string
}
type handlerToolUse struct {
	ID        string
	Name      string
	InputJSON string
}
type handlerToolResult struct {
	ToolUseID   string
	ContentText string
	IsError     bool
}
type handlerThinking struct {
	Text      string
	Signature string
}

type handlerNormalizedTool struct {
	Name        string
	Description string
	SchemaJSON  string
}

type handlerNormalizedToolChoice struct {
	Mode string
	Name string
}

func anthropicToNormalized(req *MessagesRequest) (*handlerNormalizedRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("anthropic request is nil")
	}

	out := &handlerNormalizedRequest{
		Model:           req.Model,
		Messages:        make([]handlerNormalizedMessage, 0, len(req.Messages)),
		Tools:           make([]handlerNormalizedTool, 0, len(req.Tools)),
		MaxOutputTokens: req.MaxTokens,
		Stop:            append([]string(nil), req.StopSequences...),
		Stream:          req.Stream,
	}
	if req.Temperature != nil {
		out.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		out.TopP = *req.TopP
	}
	if req.TopK != nil {
		out.TopK = *req.TopK
	}
	if req.System != nil {
		out.SystemPrompt = handlerSystemText(req.System)
	}
	if req.ToolChoice != nil {
		out.ToolChoice = handlerNormalizedToolChoice{Mode: req.ToolChoice.Type, Name: req.ToolChoice.Name}
	}
	if req.Thinking != nil && req.Thinking.Type == "enabled" {
		out.ThinkingBudget = req.Thinking.BudgetTokens
		if out.ThinkingBudget == 0 {
			out.ThinkingBudget = 4000
		}
	}

	if err := populateNormalizedRequest(out, req.Messages, req.System, req.Tools); err != nil {
		return nil, err
	}

	return out, nil
}

func countTokensToNormalized(req *CountTokensRequest) (*handlerNormalizedRequest, error) {
	if req == nil {
		return nil, fmt.Errorf("anthropic count_tokens request is nil")
	}

	out := &handlerNormalizedRequest{
		Model:    req.Model,
		Messages: make([]handlerNormalizedMessage, 0, len(req.Messages)),
		Tools:    make([]handlerNormalizedTool, 0, len(req.Tools)),
	}
	if err := populateNormalizedRequest(out, req.Messages, req.System, req.Tools); err != nil {
		return nil, err
	}
	return out, nil
}

func populateNormalizedRequest(out *handlerNormalizedRequest, messages []Message, system *SystemField, tools []Tool) error {
	if out == nil {
		return fmt.Errorf("normalized request is nil")
	}
	if system != nil {
		out.SystemPrompt = handlerSystemText(system)
	}

	for _, msg := range messages {
		parts, err := handlerMessageContentToParts(msg.Content)
		if err != nil {
			return err
		}
		out.Messages = append(out.Messages, handlerNormalizedMessage{Role: msg.Role, Parts: parts})
	}
	for _, tool := range tools {
		schema, err := json.Marshal(tool.InputSchema)
		if err != nil {
			return fmt.Errorf("marshal tool %q schema: %w", tool.Name, err)
		}
		out.Tools = append(out.Tools, handlerNormalizedTool{Name: tool.Name, Description: tool.Description, SchemaJSON: string(schema)})
	}

	return nil
}

func handlerSystemText(system *SystemField) string {
	if !system.IsArray {
		return system.String
	}
	var b strings.Builder
	for _, block := range system.Blocks {
		if text, ok := block.(TextBlock); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

func handlerMessageContentToParts(content *MessageContent) ([]handlerNormalizedPart, error) {
	if content == nil {
		return nil, nil
	}
	if !content.IsArray {
		return []handlerNormalizedPart{handlerText{Text: content.String}}, nil
	}
	parts := make([]handlerNormalizedPart, 0, len(content.Blocks))
	for _, block := range content.Blocks {
		part, err := handlerAnthropicBlockToNormalizedPart(block)
		if err != nil {
			return nil, err
		}
		parts = append(parts, part)
	}
	return parts, nil
}

func handlerAnthropicBlockToNormalizedPart(block ContentBlock) (handlerNormalizedPart, error) {
	switch b := block.(type) {
	case TextBlock:
		return handlerText{Text: b.Text}, nil
	case ImageBlock:
		return handlerImage{MediaType: b.Source.MediaType, DataB64: b.Source.Data}, nil
	case ToolUseBlock:
		return handlerToolUse{ID: b.ID, Name: b.Name, InputJSON: string(b.Input)}, nil
	case ToolResultBlock:
		isError := false
		if b.IsError != nil {
			isError = *b.IsError
		}
		return handlerToolResult{ToolUseID: b.ToolUseID, ContentText: handlerToolResultText(b.Content), IsError: isError}, nil
	case ThinkingBlock:
		return handlerThinking{Text: b.Thinking, Signature: b.Signature}, nil
	default:
		return nil, fmt.Errorf("unsupported anthropic content block %T", block)
	}
}

func handlerToolResultText(content *ToolResultContent) string {
	if content == nil {
		return ""
	}
	if !content.IsArray {
		return content.String
	}
	var b strings.Builder
	for _, block := range content.Blocks {
		if text, ok := block.(TextBlock); ok {
			b.WriteString(text.Text)
		}
	}
	return b.String()
}

func normalizedToKiro(req *handlerNormalizedRequest, profileArn string) (*kiro.KiroPayload, error) {
	if req == nil {
		return nil, fmt.Errorf("normalized request is nil")
	}

	mappedModel := kiro.MapModel(req.Model)
	lastUser := handlerLastUserMessageIndex(req.Messages)
	if lastUser == -1 {
		return nil, fmt.Errorf("normalized request has no user message")
	}

	history, err := handlerBuildKiroHistory(req.Messages[:lastUser], mappedModel)
	if err != nil {
		return nil, err
	}
	current, err := handlerBuildCurrentKiroMessage(req, req.Messages[lastUser], mappedModel)
	if err != nil {
		return nil, err
	}

	cleanHistory, cleanCurrent, _ := handlerValidatePairing(history, current)
	return &kiro.KiroPayload{
		ConversationState: kiro.ConversationState{
			ConversationID:      uuid.NewString(),
			AgentContinuationID: uuid.NewString(),
			AgentTaskType:       "vibe",
			ChatTriggerType:     "MANUAL",
			CurrentMessage:      cleanCurrent,
			History:             cleanHistory,
		},
		ProfileArn: profileArn,
	}, nil
}

func handlerLastUserMessageIndex(messages []handlerNormalizedMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return i
		}
	}
	return -1
}

func handlerBuildKiroHistory(messages []handlerNormalizedMessage, model string) ([]kiro.HistoryItem, error) {
	history := make([]kiro.HistoryItem, 0, len(messages))
	for _, msg := range messages {
		switch msg.Role {
		case "user":
			user, err := handlerBuildHistoryUserInput(msg, model)
			if err != nil {
				return nil, err
			}
			history = append(history, kiro.HistoryItem{UserInputMessage: user})
		case "tool":
			user := handlerBuildToolResultUserInput(msg)
			if user != nil {
				history = append(history, kiro.HistoryItem{UserInputMessage: user})
			}
		case "assistant":
			assistant := handlerBuildAssistantResponse(msg)
			if assistant == nil {
				continue
			}
			if len(history) > 0 && history[len(history)-1].AssistantResponseMessage != nil && len(assistant.ToolUses) > 0 {
				prev := history[len(history)-1].AssistantResponseMessage
				prev.Content += assistant.Content
				prev.ToolUses = append(prev.ToolUses, assistant.ToolUses...)
				continue
			}
			history = append(history, kiro.HistoryItem{AssistantResponseMessage: assistant})
		}
	}
	return history, nil
}

func handlerBuildHistoryUserInput(msg handlerNormalizedMessage, model string) (*kiro.UserInputMessage, error) {
	var text strings.Builder
	images := make([]kiro.ImagePart, 0)
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case handlerText:
			text.WriteString(p.Text)
		case handlerImage:
			img, err := handlerKiroImagePart(p)
			if err != nil {
				return nil, err
			}
			images = append(images, img)
		}
	}
	return &kiro.UserInputMessage{Content: text.String(), ModelID: model, Origin: "AI_EDITOR", Images: images}, nil
}

func handlerBuildToolResultUserInput(msg handlerNormalizedMessage) *kiro.UserInputMessage {
	results := make([]kiro.ToolResult, 0)
	for _, part := range msg.Parts {
		if p, ok := part.(handlerToolResult); ok {
			status := "success"
			if p.IsError {
				status = "error"
			}
			results = append(results, kiro.ToolResult{ToolUseID: p.ToolUseID, Content: []kiro.ToolResultContent{{Text: p.ContentText}}, Status: status})
		}
	}
	if len(results) == 0 {
		return nil
	}
	return &kiro.UserInputMessage{UserInputMessageContext: &kiro.UserInputMessageContext{ToolResults: results}}
}

func handlerBuildAssistantResponse(msg handlerNormalizedMessage) *kiro.AssistantResponseMessage {
	var text strings.Builder
	toolUses := make([]kiro.ToolUseEntry, 0)
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case handlerText:
			text.WriteString(p.Text)
		case handlerToolUse:
			toolUses = append(toolUses, kiro.ToolUseEntry{ToolUseID: p.ID, Name: p.Name, Input: p.InputJSON})
		}
	}
	if text.Len() == 0 && len(toolUses) == 0 {
		return nil
	}
	return &kiro.AssistantResponseMessage{Content: text.String(), ToolUses: toolUses}
}

func handlerBuildCurrentKiroMessage(req *handlerNormalizedRequest, msg handlerNormalizedMessage, model string) (kiro.CurrentMessage, error) {
	var text strings.Builder
	images := make([]kiro.ImagePart, 0)
	for _, part := range msg.Parts {
		switch p := part.(type) {
		case handlerText:
			text.WriteString(p.Text)
		case handlerImage:
			img, err := handlerKiroImagePart(p)
			if err != nil {
				return kiro.CurrentMessage{}, err
			}
			images = append(images, img)
		}
	}

	content := text.String()
	if req.ThinkingBudget > 0 {
		content = fmt.Sprintf("<thinking_mode>extended</thinking_mode><max_thinking_length>%d</max_thinking_length>%s", req.ThinkingBudget, content)
	}

	tools, err := handlerKiroTools(req.Tools)
	if err != nil {
		return kiro.CurrentMessage{}, err
	}

	var ctx *kiro.UserInputMessageContext
	if len(tools) > 0 {
		ctx = &kiro.UserInputMessageContext{Tools: tools}
	}

	return kiro.CurrentMessage{UserInputMessage: kiro.UserInputMessage{Content: content, ModelID: model, Origin: "AI_EDITOR", UserInputMessageContext: ctx, Images: images}}, nil
}

func handlerKiroTools(tools []handlerNormalizedTool) ([]kiro.Tool, error) {
	out := make([]kiro.Tool, 0, len(tools))
	for _, tool := range tools {
		schema := json.RawMessage(tool.SchemaJSON)
		if len(schema) == 0 {
			schema = json.RawMessage(`{}`)
		}
		if !json.Valid(schema) {
			return nil, fmt.Errorf("tool %q schema is invalid JSON", tool.Name)
		}
		copied := append(json.RawMessage(nil), schema...)
		out = append(out, kiro.Tool{ToolSpecification: kiro.ToolSpecification{Name: tool.Name, Description: tool.Description, InputSchema: kiro.InputSchema{JSON: copied}}})
	}
	return out, nil
}

func handlerKiroImagePart(part handlerImage) (kiro.ImagePart, error) {
	data, err := base64.StdEncoding.DecodeString(part.DataB64)
	if err != nil {
		return kiro.ImagePart{}, fmt.Errorf("decode image: %w", err)
	}
	format := part.MediaType
	if _, suffix, ok := strings.Cut(part.MediaType, "/"); ok {
		format = suffix
	}
	return kiro.ImagePart{Format: format, Source: kiro.ImageSource{Bytes: append([]byte(nil), data...)}}, nil
}

func handlerValidatePairing(history []kiro.HistoryItem, current kiro.CurrentMessage) ([]kiro.HistoryItem, kiro.CurrentMessage, []string) {
	uses := make(map[string]bool)
	results := make(map[string]bool)

	for _, item := range history {
		if item.AssistantResponseMessage != nil {
			for _, use := range item.AssistantResponseMessage.ToolUses {
				uses[use.ToolUseID] = true
			}
		}
		if item.UserInputMessage != nil && item.UserInputMessage.UserInputMessageContext != nil {
			for _, result := range item.UserInputMessage.UserInputMessageContext.ToolResults {
				results[result.ToolUseID] = true
			}
		}
	}
	if current.UserInputMessage.UserInputMessageContext != nil {
		for _, result := range current.UserInputMessage.UserInputMessageContext.ToolResults {
			results[result.ToolUseID] = true
		}
	}

	drop := make(map[string]bool)
	for id := range uses {
		if !results[id] {
			drop[id] = true
		}
	}
	for id := range results {
		if !uses[id] {
			drop[id] = true
		}
	}

	droppedIDs := make([]string, 0, len(drop))
	for id := range drop {
		droppedIDs = append(droppedIDs, id)
	}
	sort.Strings(droppedIDs)
	if len(drop) == 0 {
		return history, current, nil
	}

	cleanHistory := make([]kiro.HistoryItem, 0, len(history))
	for _, item := range history {
		clean := kiro.HistoryItem{}
		if item.AssistantResponseMessage != nil {
			assistant := *item.AssistantResponseMessage
			assistant.ToolUses = handlerFilterToolUses(assistant.ToolUses, drop)
			if assistant.Content == "" && len(assistant.ToolUses) == 0 {
				continue
			}
			clean.AssistantResponseMessage = &assistant
		}
		if item.UserInputMessage != nil {
			user := handlerCloneUserInputMessage(*item.UserInputMessage)
			if user.UserInputMessageContext != nil {
				user.UserInputMessageContext.ToolResults = handlerFilterToolResults(user.UserInputMessageContext.ToolResults, drop)
				if len(user.UserInputMessageContext.Tools) == 0 && len(user.UserInputMessageContext.ToolResults) == 0 {
					user.UserInputMessageContext = nil
				}
			}
			if user.Content == "" && len(user.Images) == 0 && user.UserInputMessageContext == nil {
				continue
			}
			clean.UserInputMessage = &user
		}
		cleanHistory = append(cleanHistory, clean)
	}

	cleanCurrent := current
	cleanCurrent.UserInputMessage = handlerCloneUserInputMessage(current.UserInputMessage)
	if cleanCurrent.UserInputMessage.UserInputMessageContext != nil {
		ctx := cleanCurrent.UserInputMessage.UserInputMessageContext
		ctx.ToolResults = handlerFilterToolResults(ctx.ToolResults, drop)
		if len(ctx.Tools) == 0 && len(ctx.ToolResults) == 0 {
			cleanCurrent.UserInputMessage.UserInputMessageContext = nil
		}
	}

	return cleanHistory, cleanCurrent, droppedIDs
}

func handlerCloneUserInputMessage(in kiro.UserInputMessage) kiro.UserInputMessage {
	out := in
	out.Images = append([]kiro.ImagePart(nil), in.Images...)
	if in.UserInputMessageContext != nil {
		ctx := *in.UserInputMessageContext
		ctx.Tools = append([]kiro.Tool(nil), in.UserInputMessageContext.Tools...)
		ctx.ToolResults = append([]kiro.ToolResult(nil), in.UserInputMessageContext.ToolResults...)
		out.UserInputMessageContext = &ctx
	}
	return out
}

func handlerFilterToolUses(in []kiro.ToolUseEntry, drop map[string]bool) []kiro.ToolUseEntry {
	out := make([]kiro.ToolUseEntry, 0, len(in))
	for _, use := range in {
		if !drop[use.ToolUseID] {
			out = append(out, use)
		}
	}
	return out
}

func handlerFilterToolResults(in []kiro.ToolResult, drop map[string]bool) []kiro.ToolResult {
	out := make([]kiro.ToolResult, 0, len(in))
	for _, result := range in {
		if !drop[result.ToolUseID] {
			out = append(out, result)
		}
	}
	return out
}

func buildResponseContent(full kiro.FullResponse) []ContentBlock {
	content := make([]ContentBlock, 0, 2+len(full.ToolUses))
	if full.Thinking != "" {
		content = append(content, ThinkingBlock{Type: "thinking", Thinking: full.Thinking, Signature: ""})
	}
	if full.Text != "" {
		content = append(content, TextBlock{Type: "text", Text: full.Text})
	}
	for _, tool := range full.ToolUses {
		input := json.RawMessage(tool.Input)
		if len(strings.TrimSpace(tool.Input)) == 0 {
			input = json.RawMessage(`{}`)
		}
		if !json.Valid(input) {
			input = json.RawMessage(`{}`)
		}
		content = append(content, ToolUseBlock{Type: "tool_use", ID: tool.ToolUseID, Name: tool.Name, Input: input})
	}
	return content
}

func normalizeStopReason(reason string, hasToolUse bool) string {
	switch reason {
	case "max_tokens":
		return "max_tokens"
	case "tool_use":
		return "tool_use"
	}
	if hasToolUse {
		return "tool_use"
	}
	return "end_turn"
}

func setSSEHeaders(c *gin.Context) {
	headers := c.Writer.Header()
	headers.Set("Content-Type", "text/event-stream")
	headers.Set("Cache-Control", "no-cache")
	headers.Set("Connection", "keep-alive")
	headers.Set("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)
}

func writeJSONError(c *gin.Context, status int, err error) {
	classified := classifyHandlerError(err)
	c.AbortWithStatusJSON(status, ErrorEvent{
		Type: "error",
		Error: ErrorDetail{
			Type:    errorClass(classified),
			Message: errorMessage(classified),
		},
	})
}

func classifyHandlerError(err error) *errs.Error {
	if err == nil {
		return errs.New(errs.ClassFatal, "UNKNOWN_ERROR", "unknown error")
	}
	var classified *errs.Error
	if errors.As(err, &classified) {
		return classified
	}
	if network := errs.FromNetwork(err); network != nil {
		return network
	}
	if errors.Is(err, account.ErrNoCandidates) || errors.Is(err, account.NoAccountsAvailable) {
		return &errs.Error{Class: errs.ClassRecoverable, Code: "NO_ACCOUNTS_AVAILABLE", HTTPStatus: http.StatusServiceUnavailable, Message: "no accounts available", Cause: err}
	}
	return errs.Wrap(err, errs.ClassFatal, err.Error())
}

const statusClientClosedRequest = 499

func statusForError(err *errs.Error) int {
	if err == nil {
		return http.StatusInternalServerError
	}
	if err.HTTPStatus > 0 {
		return err.HTTPStatus
	}
	switch err.Class {
	case errs.ClassAuthExpired:
		return http.StatusUnauthorized
	case errs.ClassRateLimited:
		return http.StatusTooManyRequests
	case errs.ClassQuotaExhausted:
		return http.StatusPaymentRequired
	case errs.ClassContentTooLong:
		return http.StatusBadRequest
	case errs.ClassRecoverable, errs.ClassNetwork:
		return http.StatusBadGateway
	case errs.ClassClientCanceled:
		return statusClientClosedRequest
	default:
		return http.StatusInternalServerError
	}
}

func markStreamActivity(activity chan<- struct{}) {
	select {
	case activity <- struct{}{}:
	default:
	}
}
