package users

import (
	"context"
	"database/sql"
	"testing"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/jackc/pgx/v5/pgtype"
)

type mockUserRepo struct {
	users            []repo.User
	getUserByNameErr error
}

func newMockUserRepo() *mockUserRepo {
	return &mockUserRepo{
		users: make([]repo.User, 0),
	}
}

func (m *mockUserRepo) GetUserByName(ctx context.Context, fullName string) ([]repo.User, error) {
	if m.getUserByNameErr != nil {
		return nil, m.getUserByNameErr
	}
	var result []repo.User
	for _, user := range m.users {
		if user.FullName == fullName {
			result = append(result, user)
		}
	}
	return result, nil
}

func TestFindByName_Success(t *testing.T) {
	mock := newMockUserRepo()
	mock.users = []repo.User{
		{
			ID:       pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true},
			FullName: "John Doe",
		},
		{
			ID:       pgtype.UUID{Bytes: [16]byte{2, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true},
			FullName: "John Smith",
		},
	}
	svc := NewService(mock, nil)

	req := FindUserRequest{FullName: "John Doe"}
	resp, err := svc.findByName(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(resp.Users) != 1 {
		t.Errorf("expected 1 user, got %d", len(resp.Users))
	}

	if resp.Users[0].FullName != "John Doe" {
		t.Errorf("expected FullName 'John Doe', got %s", resp.Users[0].FullName)
	}
}

func TestFindByName_EmptyResult(t *testing.T) {
	mock := newMockUserRepo()
	svc := NewService(mock, nil)

	req := FindUserRequest{FullName: "Nonexistent"}
	resp, err := svc.findByName(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(resp.Users) != 0 {
		t.Errorf("expected 0 users, got %d", len(resp.Users))
	}
}

func TestFindByName_PartialMatch(t *testing.T) {
	mock := newMockUserRepo()
	mock.users = []repo.User{
		{
			ID:       pgtype.UUID{Bytes: [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true},
			FullName: "John",
		},
		{
			ID:       pgtype.UUID{Bytes: [16]byte{2, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true},
			FullName: "John",
		},
		{
			ID:       pgtype.UUID{Bytes: [16]byte{3, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}, Valid: true},
			FullName: "John",
		},
	}
	svc := NewService(mock, nil)

	req := FindUserRequest{FullName: "John"}
	resp, err := svc.findByName(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(resp.Users) != 3 {
		t.Errorf("expected 3 users, got %d", len(resp.Users))
	}
}

func TestFindByName_DatabaseError(t *testing.T) {
	mock := newMockUserRepo()
	mock.getUserByNameErr = sql.ErrConnDone
	svc := NewService(mock, nil)

	req := FindUserRequest{FullName: "John"}
	_, err := svc.findByName(context.Background(), req)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
}
