package fakeapi

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthMiddleware(t *testing.T) {
	server := NewFakeMattermostServer(0, 0)
	handler := server.authMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	testCases := []struct {
		name             string
		authHeader       string
		tokenHeader      string
		wantStatus       int
		wantBodyContains string
	}{
		{
			name:       "Authorization Bearer accepted",
			authHeader: "Bearer test-token-123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Authorization BEARER accepted",
			authHeader: "BEARER test-token-123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Authorization mixed case accepted",
			authHeader: "BeArEr test-token-123",
			wantStatus: http.StatusOK,
		},
		{
			name:       "Authorization with multiple spaces accepted",
			authHeader: "Bearer    test-token-123",
			wantStatus: http.StatusOK,
		},
		{
			name:        "Token header fallback accepted",
			tokenHeader: "test-token-123",
			wantStatus:  http.StatusOK,
		},
		{
			name:             "Authorization Basic rejected",
			authHeader:       "Basic xxx",
			wantStatus:       http.StatusUnauthorized,
			wantBodyContains: "Unauthorized",
		},
		{
			name:             "Wrong bearer token rejected",
			authHeader:       "Bearer wrong-token",
			wantStatus:       http.StatusUnauthorized,
			wantBodyContains: "Invalid token",
		},
		{
			name:             "No auth headers rejected",
			wantStatus:       http.StatusUnauthorized,
			wantBodyContains: "Unauthorized",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v4/users/me", nil)
			if tc.authHeader != "" {
				req.Header.Set("Authorization", tc.authHeader)
			}
			if tc.tokenHeader != "" {
				req.Header.Set("Token", tc.tokenHeader)
			}

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d; body=%q", rec.Code, tc.wantStatus, rec.Body.String())
			}

			if tc.wantBodyContains != "" && !strings.Contains(rec.Body.String(), tc.wantBodyContains) {
				t.Fatalf("body = %q, want to contain %q", rec.Body.String(), tc.wantBodyContains)
			}
		})
	}
}
