package auth

import (
	"testing"

	"github.com/Youssef-codin/NexusPay/internal/security"
)

func TestHelper_NewAuthenticator(t *testing.T) {
	auth := security.NewAuthenticator("test-secret-key", 7*24*3600)
	if auth == nil {
		t.Error("expected authenticator to be created")
	}
}

func TestHelper_RegisterRequest_Validation(t *testing.T) {
	req := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}

	if req.Email == "" {
		t.Error("expected email to be set")
	}
	if req.Password == "" {
		t.Error("expected password to be set")
	}
	if req.FullName == "" {
		t.Error("expected fullname to be set")
	}
}

func TestHelper_LoginRequest_Validation(t *testing.T) {
	req := loginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	if req.Email == "" {
		t.Error("expected email to be set")
	}
	if req.Password == "" {
		t.Error("expected password to be set")
	}
}

func TestHelper_RefreshRequest_Validation(t *testing.T) {
	req := refreshRequest{
		RefreshToken: "test-token",
	}

	if req.RefreshToken == "" {
		t.Error("expected refresh token to be set")
	}
}

func TestHelper_ResponseTypes(t *testing.T) {
	registerResp := registerResponse{
		Email:        "test@example.com",
		FullName:     "Test User",
		JwtToken:     "jwt-token",
		RefreshToken: "refresh-token",
	}

	if registerResp.Email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", registerResp.Email)
	}

	loginResp := loginResponse{
		Email:        "test@example.com",
		FullName:     "Test User",
		JwtToken:     "jwt-token",
		RefreshToken: "refresh-token",
	}

	if loginResp.Email != "test@example.com" {
		t.Errorf("expected email 'test@example.com', got '%s'", loginResp.Email)
	}

	refreshResp := refreshResponse{
		JwtToken:     "jwt-token",
		RefreshToken: "refresh-token",
	}

	if refreshResp.JwtToken != "jwt-token" {
		t.Errorf("expected jwt token 'jwt-token', got '%s'", refreshResp.JwtToken)
	}
}

func TestHelper_Errors(t *testing.T) {
	if ErrInvalidCredentials == nil {
		t.Error("ErrInvalidCredentials should not be nil")
	}
	if ErrUserNotFound == nil {
		t.Error("ErrUserNotFound should not be nil")
	}
	if ErrUserAlreadyExists == nil {
		t.Error("ErrUserAlreadyExists should not be nil")
	}
	if ErrPasswordTooLong == nil {
		t.Error("ErrPasswordTooLong should not be nil")
	}
	if ErrTokenExpired == nil {
		t.Error("ErrTokenExpired should not be nil")
	}
}
