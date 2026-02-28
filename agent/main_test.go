package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	// Setup mock handler
	handlerCalled := false
	mockHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handlerCalled = true
		w.WriteHeader(http.StatusOK)
	})

	// Wrap handler
	middleware := authMiddleware(mockHandler)

	tests := []struct {
		name           string
		envToken       string
		requestHeader  string
		expectedStatus int
		expectCalled   bool
	}{
		{
			name:           "No token configured (open access)",
			envToken:       "",
			requestHeader:  "",
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
		{
			name:           "Token configured, missing header",
			envToken:       "secret123",
			requestHeader:  "",
			expectedStatus: http.StatusUnauthorized,
			expectCalled:   false,
		},
		{
			name:           "Token configured, wrong header",
			envToken:       "secret123",
			requestHeader:  "Bearer badtoken",
			expectedStatus: http.StatusUnauthorized,
			expectCalled:   false,
		},
		{
			name:           "Token configured, correct header",
			envToken:       "secret123",
			requestHeader:  "Bearer secret123",
			expectedStatus: http.StatusOK,
			expectCalled:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handlerCalled = false
			os.Setenv("AUTH_TOKEN", tt.envToken)
			
			req := httptest.NewRequest("GET", "/metrics", nil)
			if tt.requestHeader != "" {
				req.Header.Set("Authorization", tt.requestHeader)
			}
			rr := httptest.NewRecorder()

			middleware.ServeHTTP(rr, req)

			if status := rr.Code; status != tt.expectedStatus {
				t.Errorf("handler returned wrong status code: got %v want %v",
					status, tt.expectedStatus)
			}
			if handlerCalled != tt.expectCalled {
				t.Errorf("handler execution mismatch: got %v want %v",
					handlerCalled, tt.expectCalled)
			}
		})
	}
	os.Setenv("AUTH_TOKEN", "")
}
