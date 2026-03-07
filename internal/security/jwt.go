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
	TokenAuth *jwtauth.JWTAuth
}

func NewAuthenticator(secret string) *Authenticator {
	tokenAuth := jwtauth.New("HS256", []byte(secret), nil)
	return &Authenticator{
		TokenAuth: tokenAuth,
	}
}

func (a *Authenticator) MakeJWTToken(claims Claims) (string, error) {
	mappedClaims := map[string]interface{}{"sub": claims.ID}
	jwtauth.SetExpiry(mappedClaims, time.Now().Add(time.Minute*15))
	jwtauth.SetIssuedNow(mappedClaims)

	_, token, err := a.TokenAuth.Encode(mappedClaims)
	if err != nil {
		return "", err
	}

	return token, nil
}

func (a *Authenticator) MakeRawRefreshToken() (string, error) {
	randBytes := make([]byte, 32)

	_, err := rand.Read(randBytes)
	if err != nil {
		return "", err
	}

	return hex.EncodeToString(randBytes[:]), nil
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
				api.Error(w, err.Error(), http.StatusUnauthorized)
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
