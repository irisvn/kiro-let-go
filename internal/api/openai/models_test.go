package openai

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestModels_ReturnsSupportedModels(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.GET("/v1/models", Models)

	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)

	var resp ModelsResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, "list", resp.Object)
	require.Len(t, resp.Data, 6)

	wantIDs := []string{
		"claude-sonnet-4.5",
		"claude-sonnet-4.6",
		"claude-opus-4.5",
		"claude-opus-4.6",
		"claude-opus-4.7",
		"claude-haiku-4.5",
	}
	for i, wantID := range wantIDs {
		require.Equal(t, wantID, resp.Data[i].ID)
		require.Equal(t, "model", resp.Data[i].Object)
		require.Equal(t, modelsCreatedAt, resp.Data[i].Created)
		require.Equal(t, "kiro", resp.Data[i].OwnedBy)
	}
}
