package kiro

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/irisvn/kiro-let-go/internal/account"
	"github.com/irisvn/kiro-let-go/internal/errs"
)

type McpRequestParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type McpRequest struct {
	ID      string           `json:"id"`
	JSONRPC string           `json:"jsonrpc"`
	Method  string           `json:"method"`
	Params  McpRequestParams `json:"params"`
}

type McpContentItem struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type McpResult struct {
	Content []McpContentItem `json:"content"`
	IsError bool             `json:"isError"`
}

type McpResponseError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type McpResponse struct {
	ID      string            `json:"id"`
	JSONRPC string            `json:"jsonrpc"`
	Result  *McpResult        `json:"result,omitempty"`
	Error   *McpResponseError `json:"error,omitempty"`
}

type McpResultItem struct {
	Title         string `json:"title"`
	URL           string `json:"url"`
	Snippet       string `json:"snippet"`
	PublishedDate *int64 `json:"publishedDate,omitempty"`
}

type McpWebSearchResult struct {
	Results      []McpResultItem `json:"results"`
	TotalResults int             `json:"totalResults"`
	Query        string          `json:"query"`
}

func generateRandomID(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	// math/rand is fine for random nonces
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// CallKiroMcpAPI calls the Kiro MCP API for web search.
func CallKiroMcpAPI(ctx context.Context, client *Client, acq *account.Acquisition, query string) (string, string, *McpWebSearchResult, error) {
	if client == nil || acq == nil || acq.Account == nil {
		return "", "", nil, fmt.Errorf("invalid arguments to CallKiroMcpAPI")
	}

	random22 := generateRandomID(22)
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	random8 := generateRandomID(8)
	requestID := fmt.Sprintf("web_search_tooluse_%s_%d_%s", random22, timestamp, random8)

	// Tool use ID format like: srvtoolu_ + 32 hex chars
	uuidClean := strings.ReplaceAll(uuid.New().String(), "-", "")
	toolUseID := fmt.Sprintf("srvtoolu_%s", uuidClean)

	mcpReq := McpRequest{
		ID:      requestID,
		JSONRPC: "2.0",
		Method:  "tools/call",
		Params: McpRequestParams{
			Name: "web_search",
			Arguments: map[string]interface{}{
				"query": query,
			},
		},
	}

	payload, err := json.Marshal(mcpReq)
	if err != nil {
		return "", "", nil, errs.Wrap(err, errs.ClassFatal, "failed to marshal MCP request")
	}

	apiRegion := strings.TrimSpace(acq.Region)
	if apiRegion == "" && acq.Account.APIRegion != nil {
		apiRegion = strings.TrimSpace(*acq.Account.APIRegion)
	}
	if apiRegion == "" {
		apiRegion = strings.TrimSpace(acq.Account.Region)
	}
	if apiRegion == "" {
		apiRegion = "us-east-1"
	}

	mcpURL := fmt.Sprintf("https://runtime.%s.kiro.dev/mcp", apiRegion)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, mcpURL, bytes.NewReader(payload))
	if err != nil {
		return "", "", nil, errs.Wrap(err, errs.ClassFatal, "failed to build MCP request")
	}

	req.Header.Set("Authorization", "Bearer "+acq.Token)
	req.Header.Set("x-amzn-codewhisperer-optout", "false")
	req.Header.Set("Content-Type", "application/json")

	httpClient := client.clientForAccount(acq.Account)
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", "", nil, errs.Wrap(err, errs.ClassNetwork, "failed to perform MCP API call")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", "", nil, errs.New(errs.ClassFatal, "MCP_API_ERROR", fmt.Sprintf("MCP API returned status %d", resp.StatusCode))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", nil, errs.Wrap(err, errs.ClassFatal, "failed to read MCP API response")
	}

	var mcpResp McpResponse
	if err := json.Unmarshal(bodyBytes, &mcpResp); err != nil {
		return "", "", nil, errs.Wrap(err, errs.ClassFatal, "failed to unmarshal MCP API response")
	}

	if mcpResp.Error != nil {
		return "", "", nil, errs.New(errs.ClassFatal, "MCP_API_ERROR", fmt.Sprintf("MCP API returned error: %s (code: %d)", mcpResp.Error.Message, mcpResp.Error.Code))
	}

	if mcpResp.Result == nil || len(mcpResp.Result.Content) == 0 {
		return "", "", nil, errs.New(errs.ClassFatal, "MCP_API_ERROR", "MCP API returned empty or nil result content")
	}

	resultText := mcpResp.Result.Content[0].Text
	var searchResult McpWebSearchResult
	if err := json.Unmarshal([]byte(resultText), &searchResult); err != nil {
		return "", "", nil, errs.Wrap(err, errs.ClassFatal, "failed to unmarshal result text from MCP response")
	}

	return requestID, toolUseID, &searchResult, nil
}

// GenerateSearchSummary generates a human-readable summary from search results wrapped in XML tags.
func GenerateSearchSummary(query string, results *McpWebSearchResult) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("\n<web_search>\nSearch results for \"%s\":\n\n", query))

	if results != nil && len(results.Results) > 0 {
		for i, result := range results.Results {
			title := result.Title
			if title == "" {
				title = "Untitled"
			}
			sb.WriteString(fmt.Sprintf("%d. Title: **%s**\n", i+1, title))

			if result.PublishedDate != nil {
				// Convert millisecond timestamp to time.Time
				t := time.Unix(*result.PublishedDate/1000, (*result.PublishedDate%1000)*int64(time.Millisecond))
				// Format: 13 Mar 2025 14:23:45
				sb.WriteString(fmt.Sprintf("   Published: %s\n", t.UTC().Format("02 Jan 2006 15:04:05")))
			}

			if result.URL != "" {
				sb.WriteString(fmt.Sprintf("   URL: %s\n", result.URL))
			}

			if result.Snippet != "" {
				sb.WriteString(fmt.Sprintf("   %s\n", result.Snippet))
			}
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString("No results found.\n")
	}

	sb.WriteString("</web_search>\n")
	return sb.String()
}
