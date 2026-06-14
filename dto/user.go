package dto

type LoginRequest struct {
	Email    string `json:"email" binding:"required"`
	Password string `json:"password" binding:"required"`
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
