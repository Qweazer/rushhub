package httpx

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFailRedisUnavailable(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)

	Fail(ctx, NewRedisUnavailable(errors.New("down")))

	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("HTTP status = %d, want %d", recorder.Code, http.StatusServiceUnavailable)
	}
	var body struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response JSON: %v", err)
	}
	if body.Code != CodeRedisUnavailable {
		t.Fatalf("code = %d, want %d", body.Code, CodeRedisUnavailable)
	}
	if body.Message != "redis unavailable" {
		t.Fatalf("message = %q, want %q", body.Message, "redis unavailable")
	}
}
