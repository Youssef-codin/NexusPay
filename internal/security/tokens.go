package security

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/Youssef-codin/NexusPay/internal/utils/api"
	"github.com/go-chi/jwtauth/v5"
)

type Claims struct {
	ID string
}

type Authenticator struct {
	TokenAuth            *jwtauth.JWTAuth
	RefreshTokenDuration time.Duration
}

func NewAuthenticator(secret string, refreshTokenDuration time.Duration) *Authenticator {
	tokenAuth := jwtauth.New("HS256", []byte(secret), nil)
	return &Authenticator{
		TokenAuth:            tokenAuth,
		RefreshTokenDuration: refreshTokenDuration,
	}
}

// NOTE: sub has the ID
func (a *Authenticator) MakeJWTToken(claims Claims) string {
	mappedClaims := map[string]any{"sub": claims.ID}
	jwtauth.SetExpiry(mappedClaims, time.Now().Add(time.Minute*15))
	jwtauth.SetIssuedNow(mappedClaims)

	_, token, _ := a.TokenAuth.Encode(mappedClaims)
	return token
}

func (a *Authenticator) MakeRawRefreshToken() string {
	randBytes := make([]byte, 32)
	rand.Read(randBytes)
	return hex.EncodeToString(randBytes[:])
}

func (a *Authenticator) HashRefreshToken(token string) string {
	hash := sha256.Sum256([]byte(token))
	return hex.EncodeToString(hash[:])
}

func (a *Authenticator) AuthHandler() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		hfn := func(w http.ResponseWriter, req *http.Request) {
			token, _, err := jwtauth.FromContext(req.Context())

			if err != nil {
				api.Error(w, "unauthorized", http.StatusUnauthorized)
				return
			}

			if token == nil {
				api.Error(w, http.StatusText(http.StatusUnauthorized), http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, req)
		}
		return http.HandlerFunc(hfn)
	}
}
