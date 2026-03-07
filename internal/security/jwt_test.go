package security

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/jwtauth/v5"
)

func TestAuthHandler_ValidToken(t *testing.T) {
	auth := NewAuthenticator("test-secret")
	token, err := auth.MakeJWTToken(Claims{ID: "user123"})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(jwtauth.Verifier(auth.TokenAuth))
		r.Use(auth.AuthHandler())
		r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			w.Write([]byte("success"))
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", w.Code)
	}
}

func TestAuthHandler_NoToken(t *testing.T) {
	auth := NewAuthenticator("test-secret")

	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(auth.AuthHandler())
		r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestAuthHandler_InvalidToken(t *testing.T) {
	auth := NewAuthenticator("test-secret")

	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(auth.AuthHandler())
		r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestAuthHandler_WrongSecret(t *testing.T) {
	auth := NewAuthenticator("test-secret")
	otherAuth := NewAuthenticator("other-secret")

	token, err := otherAuth.MakeJWTToken(Claims{ID: "user123"})
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	router := chi.NewRouter()
	router.Group(func(r chi.Router) {
		r.Use(auth.AuthHandler())
		r.Get("/test", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", w.Code)
	}
}

func TestMakeJWTToken(t *testing.T) {
	auth := NewAuthenticator("test-secret")

	token, err := auth.MakeJWTToken(Claims{ID: "user123"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token == "" {
		t.Error("expected non-empty token")
	}
}

func TestHashRefreshToken(t *testing.T) {
	auth := NewAuthenticator("test-secret")

	hash1 := auth.HashRefreshToken("test-token")
	hash2 := auth.HashRefreshToken("test-token")
	hash3 := auth.HashRefreshToken("different-token")

	if len(hash1) != 64 {
		t.Errorf("expected hash length 64, got %d", len(hash1))
	}
	if hash1 != hash2 {
		t.Error("same input should produce same hash")
	}
	if hash1 == hash3 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestMakeRawRefreshToken(t *testing.T) {
	auth := NewAuthenticator("test-secret")

	token1, err := auth.MakeRawRefreshToken()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(token1) != 64 {
		t.Errorf("expected token length 64, got %d", len(token1))
	}

	token2, err := auth.MakeRawRefreshToken()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if token1 == token2 {
		t.Error("tokens should be unique")
	}
}
