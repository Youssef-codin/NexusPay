package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	repo "github.com/Youssef-codin/NexusPay/internal/db/postgresql/sqlc"
	"github.com/Youssef-codin/NexusPay/internal/security"
	"github.com/go-chi/jwtauth/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const testRefreshDuration = 7 * 24 * time.Hour

type mockQuerier struct {
	users             map[string]repo.User
	getUserByEmailErr error
	createUserErr     error
}

func newMockQuerier() *mockQuerier {
	return &mockQuerier{
		users: make(map[string]repo.User),
	}
}

func (m *mockQuerier) CreateUser(ctx context.Context, arg repo.CreateUserParams) (repo.CreateUserRow, error) {
	if m.createUserErr != nil {
		return repo.CreateUserRow{}, m.createUserErr
	}

	user := repo.User{
		ID:             pgtype.UUID{Bytes: uuid.New(), Valid: true},
		Email:          arg.Email,
		Password:       arg.Password,
		FullName:       arg.FullName,
		RefreshToken:   arg.RefreshToken,
		TokenExpiresAt: arg.TokenExpiresAt,
		CreatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
		UpdatedAt:      pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}

	m.users[arg.Email] = user
	m.users[user.RefreshToken.String] = user
	return repo.CreateUserRow{
		ID:           user.ID,
		Email:        user.Email,
		FullName:     user.FullName,
		RefreshToken: user.RefreshToken,
		CreatedAt:    user.CreatedAt,
	}, nil
}

func (m *mockQuerier) GetUserByEmail(ctx context.Context, email string) (repo.User, error) {
	if m.getUserByEmailErr != nil {
		return repo.User{}, m.getUserByEmailErr
	}
	if user, ok := m.users[email]; ok {
		return user, nil
	}
	return repo.User{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetUserById(ctx context.Context, id pgtype.UUID) (repo.User, error) {
	for _, user := range m.users {
		if user.ID.Bytes == id.Bytes {
			return user, nil
		}
	}
	return repo.User{}, pgx.ErrNoRows
}

func (m *mockQuerier) GetUserByName(ctx context.Context, fullName string) ([]repo.User, error) {
	var result []repo.User
	for _, user := range m.users {
		if user.FullName == fullName {
			result = append(result, user)
		}
	}
	return result, nil
}

func (m *mockQuerier) GetUserByRefreshToken(ctx context.Context, token pgtype.Text) (repo.User, error) {
	if user, ok := m.users[token.String]; ok {
		return user, nil
	}
	return repo.User{}, pgx.ErrNoRows
}

func (m *mockQuerier) UpdateRefreshToken(ctx context.Context, arg repo.UpdateRefreshTokenParams) error {
	for email, user := range m.users {
		if user.ID.Bytes == arg.ID.Bytes {
			user.RefreshToken = arg.RefreshToken
			user.TokenExpiresAt = arg.TokenExpiresAt
			m.users[email] = user
			m.users[arg.RefreshToken.String] = user
			return nil
		}
	}
	return pgx.ErrNoRows
}

func (m *mockQuerier) RevokeRefreshToken(ctx context.Context, id pgtype.UUID) error {
	for email, user := range m.users {
		if user.ID.Bytes == id.Bytes {
			user.RefreshToken = pgtype.Text{Valid: false}
			user.TokenExpiresAt = pgtype.Timestamptz{Valid: false}
			m.users[email] = user
			return nil
		}
	}
	return pgx.ErrNoRows
}

func (m *mockQuerier) SoftDeleteUser(ctx context.Context, id pgtype.UUID) error {
	return nil
}

func (m *mockQuerier) UpdateUserDetails(ctx context.Context, arg repo.UpdateUserDetailsParams) (repo.User, error) {
	return repo.User{}, nil
}

func newTestService(mock *mockQuerier) *Service {
	auth := security.NewAuthenticator("test-secret-key", testRefreshDuration)
	return &Service{
		repo: mock,
		auth: auth,
	}
}

func TestService_Register_Success(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	req := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}

	resp, err := svc.register(context.Background(), req)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Email != req.Email {
		t.Errorf("expected email %s, got %s", req.Email, resp.Email)
	}
	if resp.FullName != req.FullName {
		t.Errorf("expected fullname %s, got %s", req.FullName, resp.FullName)
	}
	if resp.JwtToken == "" {
		t.Error("expected JWT token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected refresh token")
	}
}

func TestService_Register_UserAlreadyExists(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	req := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}

	_, err := svc.register(context.Background(), req)
	if err != nil {
		t.Fatalf("first register should succeed, got %v", err)
	}

	_, err = svc.register(context.Background(), req)
	if !errors.Is(err, ErrUserAlreadyExists) {
		t.Errorf("expected ErrUserAlreadyExists, got %v", err)
	}
}

func TestService_Register_DatabaseError(t *testing.T) {
	mock := newMockQuerier()
	mock.getUserByEmailErr = pgx.ErrTxClosed
	svc := newTestService(mock)

	req := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}

	_, err := svc.register(context.Background(), req)
	if err != pgx.ErrTxClosed {
		t.Errorf("expected database error, got %v", err)
	}
}

func TestService_Register_PasswordTooLong(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	longPassword := string(make([]byte, 100))

	req := registerRequest{
		Email:    "test@example.com",
		Password: longPassword,
		FullName: "Test User",
	}

	_, err := svc.register(context.Background(), req)
	if !errors.Is(err, ErrPasswordTooLong) {
		t.Errorf("expected ErrPasswordTooLong, got %v", err)
	}
}

func TestService_Login_Success(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	registerReq := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}
	svc.register(context.Background(), registerReq)

	loginReq := loginRequest{
		Email:    "test@example.com",
		Password: "password123",
	}

	resp, err := svc.login(context.Background(), loginReq)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if resp.Email != loginReq.Email {
		t.Errorf("expected email %s, got %s", loginReq.Email, resp.Email)
	}
	if resp.JwtToken == "" {
		t.Error("expected JWT token")
	}
	if resp.RefreshToken == "" {
		t.Error("expected refresh token")
	}
}

func TestService_Login_UserNotFound(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	req := loginRequest{
		Email:    "nonexistent@example.com",
		Password: "password123",
	}

	_, err := svc.login(context.Background(), req)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestService_Login_InvalidPassword(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	registerReq := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}
	svc.register(context.Background(), registerReq)

	loginReq := loginRequest{
		Email:    "test@example.com",
		Password: "wrongpassword",
	}

	_, err := svc.login(context.Background(), loginReq)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Errorf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestService_RefreshToken_Success(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	registerReq := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}
	resp, _ := svc.register(context.Background(), registerReq)

	refreshReq := refreshRequest{
		RefreshToken: resp.RefreshToken,
	}

	refreshResp, err := svc.refreshToken(context.Background(), refreshReq)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if refreshResp.JwtToken == "" {
		t.Error("expected JWT token")
	}
	if refreshResp.RefreshToken == "" {
		t.Error("expected refresh token")
	}
	if refreshResp.RefreshToken == resp.RefreshToken {
		t.Error("refresh token should be different")
	}
}

func TestService_RefreshToken_InvalidToken(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	req := refreshRequest{
		RefreshToken: "invalid-token",
	}

	_, err := svc.refreshToken(context.Background(), req)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func TestService_RefreshToken_ExpiredToken(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	registerReq := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}
	resp, _ := svc.register(context.Background(), registerReq)

	for key, user := range mock.users {
		user.TokenExpiresAt = pgtype.Timestamptz{
			Time:  time.Now().Add(-1 * time.Hour),
			Valid: true,
		}
		mock.users[key] = user
	}

	refreshReq := refreshRequest{
		RefreshToken: resp.RefreshToken,
	}

	_, err := svc.refreshToken(context.Background(), refreshReq)
	if !errors.Is(err, ErrTokenExpired) {
		t.Errorf("expected ErrTokenExpired, got %v", err)
	}
}

func TestService_Logout_Success(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	registerReq := registerRequest{
		Email:    "test@example.com",
		Password: "password123",
		FullName: "Test User",
	}
	svc.register(context.Background(), registerReq)

	user := mock.users["test@example.com"]

	ctx := contextWithJWT(context.Background(), user.ID.Bytes[:], svc.auth)
	err := svc.logout(ctx)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	updatedUser := mock.users["test@example.com"]
	if updatedUser.RefreshToken.Valid {
		t.Error("refresh token should be revoked")
	}
}

func TestService_Logout_UserNotFound(t *testing.T) {
	mock := newMockQuerier()
	svc := newTestService(mock)

	invalidID := uuid.New()
	ctx := contextWithJWT(context.Background(), invalidID[:], svc.auth)
	err := svc.logout(ctx)
	if !errors.Is(err, ErrUserNotFound) {
		t.Errorf("expected ErrUserNotFound, got %v", err)
	}
}

func contextWithJWT(ctx context.Context, userID []byte, auth *security.Authenticator) context.Context {
	claims := map[string]interface{}{"sub": uuid.UUID(userID).String()}
	jwtToken, _, _ := auth.TokenAuth.Encode(claims)
	return jwtauth.NewContext(ctx, jwtToken, nil)
}
