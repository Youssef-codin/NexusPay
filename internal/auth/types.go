package auth

type registerRequest struct {
	Email    string `json:"email"     validate:"required,email"`
	Password string `json:"password"  validate:"required"`
	FullName string `json:"full_name" validate:"required,min=3"`
}

type registerResponse struct {
	Email        string `json:"email"`
	FullName     string `json:"full_name"`
	JwtToken     string `json:"jwt_token"`
	RefreshToken string `json:"-"`
}

type loginResponse struct {
	Email        string `json:"email"`
	FullName     string `json:"full_name"`
	JwtToken     string `json:"jwt_token"`
	RefreshToken string `json:"-"`
}

type loginRequest struct {
	Email    string `json:"email"    validate:"required,email"`
	Password string `json:"password" validate:"required"`
}

type refreshRequest struct {
	RefreshToken string
}

type refreshResponse struct {
	JwtToken     string `json:"jwt_token"`
	RefreshToken string `json:"-"`
}
