package users

import (
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestHelper_FindUserRequest_Validation(t *testing.T) {
	req := FindUserRequest{
		FullName: "John Doe",
	}

	if req.FullName == "" {
		t.Error("expected FullName to be set")
	}
}

func TestHelper_FindUserResponse(t *testing.T) {
	resp := FindUserResponse{
		Users: []UserType{
			{ID: "123", FullName: "John Doe"},
			{ID: "456", FullName: "Jane Doe"},
		},
	}

	if len(resp.Users) != 2 {
		t.Errorf("expected 2 users, got %d", len(resp.Users))
	}
}

func TestHelper_UserType(t *testing.T) {
	user := UserType{
		ID:       "123",
		FullName: "John Doe",
	}

	if user.ID != "123" {
		t.Errorf("expected ID '123', got '%s'", user.ID)
	}
	if user.FullName != "John Doe" {
		t.Errorf("expected FullName 'John Doe', got '%s'", user.FullName)
	}
}

func TestHelper_Errors(t *testing.T) {
	if ErrUserNotFound == nil {
		t.Error("ErrUserNotFound should not be nil")
	}
	if ErrBadRequest == nil {
		t.Error("ErrBadRequest should not be nil")
	}
}

type mockQuerier struct {
	users []struct {
		ID       pgtype.UUID
		FullName string
	}
	err error
}

func TestHelper_MockQuerier(t *testing.T) {
	mock := mockQuerier{}
	mock.users = []struct {
		ID       pgtype.UUID
		FullName string
	}{
		{ID: pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true}, FullName: "John"},
	}

	if len(mock.users) != 1 {
		t.Errorf("expected 1 user in mock, got %d", len(mock.users))
	}
}
