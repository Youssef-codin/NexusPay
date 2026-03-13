package users

type FindUserRequest struct {
	FullName string `json:"full_name" validate:"required,min=2,max=100"`
}

type FindUserResponse struct {
	Users []UserType
}

type UserType struct {
	ID       string `json:"id"`
	FullName string `json:"full_name"`
}
