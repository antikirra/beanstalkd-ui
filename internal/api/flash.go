package api

import (
	"encoding/base64"
	"net/http"
	"strings"
	"time"
)

const flashCookieName = "beanstalkd_ui_flash"

type flash struct {
	Type    string
	Message string
}

func setFlash(w http.ResponseWriter, typ, message string) {
	value := base64.StdEncoding.EncodeToString([]byte(typ + ":" + message))
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   60,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
	})
}

func getFlash(w http.ResponseWriter, r *http.Request) *flash {
	c, err := r.Cookie(flashCookieName)
	if err != nil {
		return nil
	}
	// Clear immediately.
	http.SetCookie(w, &http.Cookie{
		Name:     flashCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Unix(0, 0),
	})
	data, err := base64.StdEncoding.DecodeString(c.Value)
	if err != nil {
		return nil
	}
	typ, msg, ok := strings.Cut(string(data), ":")
	if !ok {
		return nil
	}
	return &flash{Type: typ, Message: msg}
}
