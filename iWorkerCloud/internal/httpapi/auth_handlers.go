package httpapi

import (
	"net/http"

	"github.com/RapidAI/CodeClaw/iWorkerCloud/internal/auth"
)

func AdminStatusHandler(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ok, _ := svc.IsSetup(r.Context())
		writeJSON(w, http.StatusOK, map[string]bool{"setup": ok})
	}
}

func SetupHandler(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "请求格式错误")
			return
		}
		if req.Username == "" || req.Password == "" {
			writeError(w, http.StatusBadRequest, "INVALID_INPUT", "用户名和密码不能为空")
			return
		}
		if err := svc.Setup(r.Context(), req.Username, req.Password); err != nil {
			writeError(w, http.StatusConflict, "ALREADY_SETUP", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

func CaptchaHandler(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		c := svc.GenerateCaptcha()
		writeJSON(w, http.StatusOK, c)
	}
}

func LoginHandler(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username      string `json:"username"`
			Password      string `json:"password"`
			CaptchaID     string `json:"captcha_id"`
			CaptchaAnswer string `json:"captcha_answer"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "请求格式错误")
			return
		}
		token, err := svc.Login(r.Context(), req.Username, req.Password, req.CaptchaID, req.CaptchaAnswer)
		if err != nil {
			code := "LOGIN_FAILED"
			if err == auth.ErrInvalidCaptcha {
				code = "CAPTCHA_FAILED"
			}
			writeError(w, http.StatusUnauthorized, code, err.Error())
			return
		}
		StoreSession(token, req.Username)
		http.SetCookie(w, &http.Cookie{
			Name:     "session",
			Value:    token,
			Path:     "/",
			HttpOnly: true,
			MaxAge:   86400,
		})
		writeJSON(w, http.StatusOK, map[string]string{"token": token})
	}
}

func ChangePasswordHandler(svc *auth.Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Username    string `json:"username"`
			OldPassword string `json:"old_password"`
			NewPassword string `json:"new_password"`
		}
		if err := decodeJSON(r, &req); err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_JSON", "请求格式错误")
			return
		}
		if err := svc.ChangePassword(r.Context(), req.Username, req.OldPassword, req.NewPassword); err != nil {
			writeError(w, http.StatusBadRequest, "CHANGE_FAILED", err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
