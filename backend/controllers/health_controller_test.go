package controllers

import (
	"encoding/json"
	"meerkat/clock"
	"meerkat/middleware"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func TestHealthCheck(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create a test database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Create a test router
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})
	router.GET("/health", HealthCheck)

	// Create a test request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	// Assert status code
	if w.Code != http.StatusOK {
		t.Errorf("Expected status %d, got %d", http.StatusOK, w.Code)
	}

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Assert response fields
	if response.Status != "healthy" {
		t.Errorf("Expected status 'healthy', got '%s'", response.Status)
	}

	if response.Database.Status != "healthy" {
		t.Errorf("Expected database status 'healthy', got '%s'", response.Database.Status)
	}

	if response.Version == "" {
		t.Error("Expected version to be set")
	}

	if response.Timestamp == "" {
		t.Error("Expected timestamp to be set")
	}

	if response.Database.ResponseTime < 0 {
		t.Error("Expected response time to be >= 0")
	}
}

func TestHealthCheck_UnhealthyDatabase(t *testing.T) {
	// Set Gin to test mode
	gin.SetMode(gin.TestMode)

	// Create a database that will be closed
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	// Close the database to simulate unhealthy state
	sqlDB, _ := db.DB()
	sqlDB.Close()

	// Create a test router
	router := gin.Default()
	router.Use(func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})
	router.GET("/health", HealthCheck)

	// Create a test request
	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	// Assert status code
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("Expected status %d, got %d", http.StatusServiceUnavailable, w.Code)
	}

	// Parse response
	var response HealthResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	if err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}

	// Assert response fields
	if response.Status != "unhealthy" {
		t.Errorf("Expected status 'unhealthy', got '%s'", response.Status)
	}

	if response.Database.Status != "unhealthy" {
		t.Errorf("Expected database status 'unhealthy', got '%s'", response.Database.Status)
	}
}

// The request clock, not the wall clock, decides the time a handler reports.
func TestHealthCheckReportsTheRequestClock(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}
	fake := clock.NewFake(time.Date(2031, 2, 3, 4, 5, 6, 0, time.UTC))
	router := gin.New()
	router.Use(middleware.ClockMiddleware(fake), func(c *gin.Context) {
		c.Set("db", db)
		c.Next()
	})
	router.GET("/health", HealthCheck)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/health", nil)
	router.ServeHTTP(w, req)

	var response HealthResponse
	if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if response.Timestamp != "2031-02-03T04:05:06Z" {
		t.Errorf("Timestamp = %q, want the fake clock's time", response.Timestamp)
	}
}
