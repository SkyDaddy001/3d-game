package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"
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
			// A new visitor. Cap how many one address can create, so a script cannot fill memory.
			if s.limits != nil && !s.limits.allow("device|ip|"+s.clientIP(r), 120, time.Hour) {
				http.Error(w, "too many new visitors from this address, try again later", http.StatusTooManyRequests)
				return
			}
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
	case errors.Is(err, ErrNameTaken), errors.Is(err, ErrEmailTaken), errors.Is(err, ErrAlreadyFriend), errors.Is(err, ErrHasAccount):
		status = http.StatusConflict
	case errors.Is(err, ErrCooldown), errors.Is(err, ErrOTPTries):
		status = http.StatusTooManyRequests
	case errors.Is(err, ErrNotVerified), errors.Is(err, ErrNoName), errors.Is(err, ErrKeepAccount):
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
	if s.limited(w, r, "name", 10, 10*time.Minute) {
		return
	}
	if _, err := s.store.SetName(sessionFrom(r).token, r.FormValue("name")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// sendCode emails a one-time code to the address the player enters.
func (s *Server) sendCode(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "code", 5, 10*time.Minute) {
		return
	}
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
	if s.limited(w, r, "otp", 12, 10*time.Minute) {
		return
	}
	if err := s.store.CheckOTP(sessionFrom(r).token, r.FormValue("code")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// addFriend adds a player by usertag.
func (s *Server) addFriend(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "friend", 20, time.Minute) {
		return
	}
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
		name := html.EscapeString(f)
		b.WriteString(`<li><span>` + name + `</span> <button class="mini" hx-post="/friends/remove" hx-vals='{"usertag":"` + name +
			`"}' hx-swap="none" hx-confirm="Remove ` + name + ` from your friends?">Remove</button></li>`)
	}
	b.WriteString("</ul>")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprint(w, b.String())
}

// loginEmail starts signing this device in to an existing account. The answer is the same
// whether or not the address has an account.
func (s *Server) loginEmail(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "login", 5, 10*time.Minute) {
		return
	}
	if s.mailer == nil {
		http.Error(w, "email sign-in is not configured on this server", http.StatusServiceUnavailable)
		return
	}
	code, addr, send, err := s.store.BeginLogin(sessionFrom(r).token, r.FormValue("email"))
	if err != nil {
		httpError(w, err)
		return
	}
	if send {
		body := fmt.Sprintf("Your Arena Royale sign-in code is %s. It expires in 10 minutes.\n\n"+
			"Someone is signing in to your account on a new device. If that was not you, ignore this email.", code)
		if err := s.mailer.Send(addr, "Sign in to Arena Royale on a new device", body); err != nil {
			http.Error(w, "could not send the code, try again shortly", http.StatusBadGateway)
			return
		}
	}
	fmt.Fprint(w, "If that email has an account, a code is on its way.")
}

// loginCode finishes signing in on a new device.
func (s *Server) loginCode(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "loginotp", 12, 10*time.Minute) {
		return
	}
	if _, err := s.store.CompleteLogin(sessionFrom(r).token, r.FormValue("code")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// signOut removes this device from the account and forgets its cookie.
func (s *Server) signOut(w http.ResponseWriter, r *http.Request) {
	if err := s.store.SignOut(sessionFrom(r).token); err != nil {
		httpError(w, err)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: deviceCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: s.secureCookie, SameSite: http.SameSiteLaxMode})
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// signOutOthers signs every other device out of the account.
func (s *Server) signOutOthers(w http.ResponseWriter, r *http.Request) {
	n, err := s.store.SignOutOthers(sessionFrom(r).token)
	if err != nil {
		httpError(w, err)
		return
	}
	fmt.Fprintf(w, "Signed out %d other device(s).", n)
}

func (s *Server) removeFriend(w http.ResponseWriter, r *http.Request) {
	if err := s.store.RemoveFriend(sessionFrom(r).token, r.FormValue("usertag")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) blockPlayer(w http.ResponseWriter, r *http.Request) {
	if s.limited(w, r, "block", 20, time.Minute) {
		return
	}
	if err := s.store.Block(sessionFrom(r).token, r.FormValue("usertag")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) unblockPlayer(w http.ResponseWriter, r *http.Request) {
	if err := s.store.Unblock(sessionFrom(r).token, r.FormValue("usertag")); err != nil {
		httpError(w, err)
		return
	}
	w.Header().Set("HX-Redirect", "/")
	w.WriteHeader(http.StatusOK)
}

// reportPlayer saves a complaint for the operator to read.
func (s *Server) reportPlayer(w http.ResponseWriter, r *http.Request) {
	if !requirePlayer(w, r) {
		return
	}
	if s.limited(w, r, "report", 5, 10*time.Minute) {
		return
	}
	target, ok := s.store.Exists(r.FormValue("usertag"))
	reason := r.FormValue("reason")
	me := sessionFrom(r).acc.Name
	if !ok || target == me {
		http.Error(w, "no other player has that usertag", http.StatusNotFound)
		return
	}
	if !reportReasons[reason] {
		http.Error(w, "choose a reason", http.StatusUnprocessableEntity)
		return
	}
	note := strings.TrimSpace(r.FormValue("note"))
	if len(note) > 300 {
		note = note[:300]
	}
	if err := s.reports.add(Report{Reporter: me, Target: target, Reason: reason, Note: note}); err != nil {
		http.Error(w, "could not save the report, try again", http.StatusInternalServerError)
		return
	}
	fmt.Fprint(w, "Report sent. Thank you.")
}
