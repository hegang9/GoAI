package dto

type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginResponse struct {
	Response
	Token string `json:"token,omitempty"`
}

type RegisterRequest struct {
	Email    string `json:"email" binding:"required"`
	Captcha  string `json:"captcha"`
	Password string `json:"password"`
}

type RegisterResponse struct {
	Response
	Token string `json:"token,omitempty"`
}

type CaptchaRequest struct {
	Email string `json:"email" binding:"required"`
}

type CaptchaResponse struct {
	Response
}
