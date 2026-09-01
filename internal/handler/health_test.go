package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/gin-gonic/gin"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type fakeRedisPinger struct {
	err error
}

func (f fakeRedisPinger) Ping(context.Context) error {
	return f.err
}

func TestHealthHealthy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock := healthTestDB(t)
	mock.ExpectExec("^SELECT 1$").WillReturnResult(sqlmock.NewResult(0, 0))

	response := performHealthRequest(db, fakeRedisPinger{})

	if response.Code != http.StatusOK {
		t.Fatalf("status code = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	body := decodeHealthResponse(t, response)
	if body.Status != "ok" {
		t.Fatalf("status = %q, want ok", body.Status)
	}
	if body.Checks.DB != "ok" {
		t.Fatalf("checks.db = %q, want ok", body.Checks.DB)
	}
	if body.Checks.Redis != "ok" {
		t.Fatalf("checks.redis = %q, want ok", body.Checks.Redis)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database health check: %v", err)
	}
}

func TestHealthRedisFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock := healthTestDB(t)
	mock.ExpectExec("^SELECT 1$").WillReturnResult(sqlmock.NewResult(0, 0))

	response := performHealthRequest(db, fakeRedisPinger{err: errors.New("redis offline")})

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d; body = %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	body := decodeHealthResponse(t, response)
	if body.Status != "down" {
		t.Fatalf("status = %q, want down", body.Status)
	}
	if body.Checks.DB != "ok" {
		t.Fatalf("checks.db = %q, want ok", body.Checks.DB)
	}
	if !strings.HasPrefix(body.Checks.Redis, "down:") {
		t.Fatalf("checks.redis = %q, want down: prefix", body.Checks.Redis)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database health check: %v", err)
	}
}

func TestHealthDatabaseFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, mock := healthTestDB(t)
	mock.ExpectExec("^SELECT 1$").WillReturnError(errors.New("mysql offline"))

	response := performHealthRequest(db, fakeRedisPinger{})

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status code = %d, want %d; body = %s", response.Code, http.StatusServiceUnavailable, response.Body.String())
	}
	body := decodeHealthResponse(t, response)
	if body.Status != "down" {
		t.Fatalf("status = %q, want down", body.Status)
	}
	if !strings.HasPrefix(body.Checks.DB, "down:") {
		t.Fatalf("checks.db = %q, want down: prefix", body.Checks.DB)
	}
	if body.Checks.Redis != "ok" {
		t.Fatalf("checks.redis = %q, want ok", body.Checks.Redis)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("database health check: %v", err)
	}
}

func healthTestDB(t *testing.T) (*gorm.DB, sqlmock.Sqlmock) {
	t.Helper()

	sqlDB, mock, err := sqlmock.New()
	if err != nil {
		t.Fatalf("create sqlmock: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	db, err := gorm.Open(mysql.New(mysql.Config{
		Conn:                      sqlDB,
		SkipInitializeWithVersion: true,
	}), &gorm.Config{})
	if err != nil {
		t.Fatalf("open GORM database: %v", err)
	}
	return db, mock
}

func performHealthRequest(db *gorm.DB, redis fakeRedisPinger) *httptest.ResponseRecorder {
	router := gin.New()
	router.GET("/health", NewHealthHandler(db, redis).Handle)
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/health", nil)
	router.ServeHTTP(response, request)
	return response
}

func decodeHealthResponse(t *testing.T, response *httptest.ResponseRecorder) struct {
	Status string `json:"status"`
	Checks struct {
		DB    string `json:"db"`
		Redis string `json:"redis"`
	} `json:"checks"`
} {
	t.Helper()
	var body struct {
		Status string `json:"status"`
		Checks struct {
			DB    string `json:"db"`
			Redis string `json:"redis"`
		} `json:"checks"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode health response: %v", err)
	}
	return body
}
