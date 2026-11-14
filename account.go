package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
)

// deviceCookie holds the secret that logs a browser into its account. It is HttpOnly, so page
// scripts cannot read it. With ARENA_SECURE_COOKIE=1 it is also marked Secure, for HTTPS sites.
const deviceCookie = "arena_device"

type session struct {
	token string
	acc   Account
}

type sessionKey struct{}

// sessionFrom returns the request's session. It is only valid behind withAccount.
func sessionFrom(r *http.Request) session {
	s, _ := r.Context().Value(sessionKey{}).(session)
	return s
}

// withAccount gives every visitor an account. A new visitor gets an account and a cookie; a
// returning visitor is recognised by the cookie and keeps the same profile.
func (s *Server) withAccount(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var sess session
		if c, err := r.Cookie(deviceCookie); err == nil {
			if acc, ok := s.store.ByToken(c.Value); ok {
				sess = session{token: c.Value, acc: acc}
			}
		}
		if sess.token == "" {
			token, acc, err := s.store.NewDevice()
			if err != nil {
				http.Error(w, "could not create account", http.StatusInternalServerError)
				return
			}
			sess = session{token: token, acc: acc}
			http.SetCookie(w, &http.Cookie{
				Name:     deviceCookie,
				Value:    token,
				Path:     "/",
				MaxAge:   365 * 24 * 60 * 60,
				HttpOnly: true,
				Secure:   s.secureCookie,
				SameSite: http.SameSiteLaxMode,
			})
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), sessionKey{}, sess)))
	})
}

// ready reports whether the session may play: it has a usertag and a verified email.
func ready(r *http.Request) bool {
	acc := sessionFrom(r).acc
	return acc.Name != "" && acc.Verified
}

// httpError maps the store's errors to HTTP statuses that the lobby can show.
func httpError(w http.ResponseWriter, err error) {
	status := http.StatusUnprocessableEntity
	switch {
	case errors.Is(err, ErrNameTaken), errors.Is(err, ErrEmailTaken), errors.Is(err, ErrAlreadyFriend):
		status = http.StatusConflict
	case errors.Is(err, ErrCooldown), errors.Is(err, ErrOTPTries):
		status = http.StatusTooManyRequests
	case errors.Is(err, ErrNotVerified), errors.Is(err, ErrNoName):
		status = http.StatusForbidden
	case errors.Is(err, ErrNoUser):
		status = http.StatusNotFound
	case errors.Is(err, ErrUnknown):
		status = http.StatusUnauthorized
	}
	http.Error(w, err.Error(), status)
}

// setName handles the usertag form. A device can name itself once.
func (s *Server) setName(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.SetName(sessionFrom(r).token, r.FormValue("name")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// sendCode emails a one-time code to the address the player enters.
func (s *Server) sendCode(w http.ResponseWriter, r *http.Request) {
	if s.mailer == nil {
		http.Error(w, "email sign-in is not configured on this server", http.StatusServiceUnavailable)
		return
	}
	code, addr, err := s.store.BeginVerify(sessionFrom(r).token, r.FormValue("email"))
	if err != nil {
		httpError(w, err)
		return
	}
	body := fmt.Sprintf("Your Arena Royale code is %s. It expires in 10 minutes.\n\n"+
		"If you did not ask for this, you can ignore this email.", code)
	if err := s.mailer.Send(addr, "Your Arena Royale sign-in code", body); err != nil {
		http.Error(w, "could not send the code, try again shortly", http.StatusBadGateway)
		return
	}
	fmt.Fprint(w, "Code sent. Check your inbox.")
}

// verifyCode checks the code the player typed.
func (s *Server) verifyCode(w http.ResponseWriter, r *http.Request) {
	if err := s.store.CheckOTP(sessionFrom(r).token, r.FormValue("code")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// addFriend adds a player by usertag.
func (s *Server) addFriend(w http.ResponseWriter, r *http.Request) {
	if err := s.store.AddFriend(sessionFrom(r).token, r.FormValue("usertag")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// friendsList renders the friends list as HTML. Usertags are escaped before they are written.
func (s *Server) friendsList(w http.ResponseWriter, r *http.Request) {
	friends := sessionFrom(r).acc.Friends
	if len(friends) == 0 {
		fmt.Fprint(w, `<p class="empty">No friends yet. Add one by usertag.</p>`)
		return
	}
	var b strings.Builder
	b.WriteString(`<ul class="friends">`)
	for _, f := range friends {
		b.WriteString("<li>" + html.EscapeString(f) + "</li>")
	}
	b.WriteString("</ul>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, b.String())
}
