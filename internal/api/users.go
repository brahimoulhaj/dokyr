package api

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"time"

	"panel/internal/account"
	"panel/internal/brand"
	"panel/internal/store"
)

type inviteJSON struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expires_at"`
	CreatedAt time.Time `json:"created_at"`
	Expired   bool      `json:"expired"`
	// Emailed says the link was sent to the invited address; EmailError why it was not,
	// when sending was asked for. Both only in the response that creates the invite.
	Emailed    bool   `json:"emailed,omitempty"`
	EmailError string `json:"email_error,omitempty"`
	// Token is the secret of the invite link, present only in the response that creates it.
	Token string `json:"token,omitempty"`
}

func toInviteJSON(i store.Invite) inviteJSON {
	return inviteJSON{ID: i.ID, Email: i.Email, Role: i.Role, ExpiresAt: i.ExpiresAt, CreatedAt: i.CreatedAt, Expired: !time.Now().Before(i.ExpiresAt)}
}

// userError answers the account package's rule violations; false when err is nil.
func (s *Server) userError(w http.ResponseWriter, what string, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, account.ErrInvalidEmail), errors.Is(err, account.ErrWeakPassword), errors.Is(err, account.ErrInvalidRole):
		invalid(w, err.Error())
	case errors.Is(err, account.ErrEmailTaken):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, account.ErrAlreadyOwner):
		writeError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, account.ErrOwnerProtected):
		writeError(w, http.StatusForbidden, "forbidden", err.Error())
	case errors.Is(err, account.ErrBadInvite):
		writeError(w, http.StatusForbidden, "bad_invite", err.Error())
	default:
		return s.storeError(w, what, err, "")
	}
	return true
}

func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListUsers(r.Context())
	if err != nil {
		s.internal(w, "list users", err)
		return
	}
	out := make([]userJSON, 0, len(list))
	for _, u := range list {
		out = append(out, toUserJSON(u))
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleUpdateUser(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role string `json:"role"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	u, err := s.accounts.SetRole(r.Context(), r.PathValue("id"), in.Role)
	if s.userError(w, "update user", err) {
		return
	}
	s.audit(r, "user.role", "user", u.ID, u.Email, u.Role)
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

func (s *Server) handleDeleteUser(w http.ResponseWriter, r *http.Request) {
	u, err := s.accounts.RemoveUser(r.Context(), r.PathValue("id"))
	if s.userError(w, "delete user", err) {
		return
	}
	s.audit(r, "user.remove", "user", u.ID, u.Email, "")
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	list, err := s.store.ListInvites(r.Context())
	if err != nil {
		s.internal(w, "list invites", err)
		return
	}
	out := make([]inviteJSON, 0, len(list))
	for _, i := range list {
		out = append(out, toInviteJSON(i))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleInviteMail says whether the panel can email an invitation itself: it needs an
// email notification channel for its mail server and a panel address to put in the link.
func (s *Server) handleInviteMail(w http.ResponseWriter, r *http.Request) {
	out := map[string]any{"available": false, "channel": ""}
	if s.notifier != nil {
		if name, ok := s.notifier.Mailer(r.Context()); ok {
			_, err := s.loginOrigin(r)
			out["available"], out["channel"] = err == nil, name
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleCreateInvite returns the link token once; the owner passes the link on
// themselves, or with send the panel emails it to the invited address. The invite stands
// even when the mail fails: the answer says so and still carries the link.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Email string `json:"email"`
		Role  string `json:"role"`
		Send  bool   `json:"send"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	origin := ""
	if in.Send {
		// Checked before the invite is made: asking for a mail the panel cannot send is a
		// mistake to report, not an invite to create quietly without it.
		if s.notifier == nil {
			invalid(w, "add an email notification channel before sending invites by email")
			return
		}
		if _, ok := s.notifier.Mailer(r.Context()); !ok {
			invalid(w, "add an email notification channel before sending invites by email")
			return
		}
		// The link goes into a mail: only an address the panel is configured to answer on.
		o, err := s.loginOrigin(r)
		if err != nil {
			invalid(w, "open the panel on its own HTTPS domain before sending invites by email")
			return
		}
		origin = o
	}
	inv, token, err := s.accounts.Invite(r.Context(), userFrom(r.Context()).ID, in.Email, in.Role)
	if s.userError(w, "create invite", err) {
		return
	}
	s.audit(r, "user.invite", "user", inv.ID, inv.Email, inv.Role)
	out := toInviteJSON(inv)
	out.Token = token
	if in.Send {
		inviter := userFrom(r.Context()).Email
		text := inviter + " invites you to " + brand.Name + " as " + inv.Role + ".\n\n" +
			"Open this link to choose a password and join:\n" + origin + "/invite?token=" + url.QueryEscape(token) + "\n\n" +
			"The link works once and expires on " + inv.ExpiresAt.UTC().Format("2 January 2006, 15:04 UTC") + ". If you did not expect it, ignore this message."
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		if err := s.notifier.Mail(ctx, inv.Email, "You are invited to "+brand.Name, text); err != nil {
			s.log.Warn("invite mail", "to", inv.Email, "error", err)
			out.EmailError = err.Error()
		} else {
			out.Emailed = true
		}
	}
	writeJSON(w, http.StatusCreated, out)
}

// handleTransferOwnership makes another member the owner and the caller an admin. It asks
// for the caller's password again, or a passkey confirmation made just before: the panel
// changes hands, and a session left open must not be enough for that.
func (s *Server) handleTransferOwnership(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	if in.Password == "" {
		if !s.confirmed.take(sessionHash(r)) {
			writeError(w, http.StatusUnauthorized, "confirm_required", "confirm with your password or a passkey")
			return
		}
	} else if !s.confirmLoginPassword(w, r, in.Password) {
		return
	}
	me := userFrom(r.Context())
	u, err := s.accounts.TransferOwnership(r.Context(), me.ID, r.PathValue("id"))
	if s.userError(w, "transfer ownership", err) {
		return
	}
	s.audit(r, "user.ownership", "user", u.ID, u.Email, "from "+me.Email)
	writeJSON(w, http.StatusOK, toUserJSON(u))
}

func (s *Server) handleDeleteInvite(w http.ResponseWriter, r *http.Request) {
	inv, err := s.store.DeleteInvite(r.Context(), r.PathValue("id"))
	if s.storeError(w, "delete invite", err, "") {
		return
	}
	s.audit(r, "user.invite_revoke", "user", inv.ID, inv.Email, "")
	w.WriteHeader(http.StatusNoContent)
}

// handleLookupInvite is public: the invite page shows who the link is for before asking
// for a password. The token travels in the body so it stays out of access logs.
func (s *Server) handleLookupInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token string `json:"token"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	inv, err := s.accounts.InviteByToken(r.Context(), in.Token)
	if s.userError(w, "invite lookup", err) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"email": inv.Email, "role": inv.Role})
}

func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Token    string `json:"token"`
		Password string `json:"password"`
	}
	if !readJSON(w, r, &in) {
		return
	}
	u, err := s.accounts.AcceptInvite(r.Context(), in.Token, in.Password)
	if s.userError(w, "accept invite", err) {
		return
	}
	if err := s.store.AddEvent(r.Context(), store.Event{UserID: u.ID, Actor: u.Email, Action: "user.join", TargetType: "user", TargetID: u.ID, TargetName: u.Email, Detail: u.Role}); err != nil {
		s.log.Error("record event", "action", "user.join", "error", err)
	}
	s.startSession(w, r, u, http.StatusCreated)
}
