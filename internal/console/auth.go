package console

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"strings"
)

const passwordIterations = 600000
const dummyPasswordHash = "pbkdf2-sha256$600000$AAECAwQFBgcICQoLDA0ODw$NyjOjly17HG8sQZN6G_bCa7m5kd0W5PAaUE7jUNwNOw"

func HashPassword(pw string) (string, error) {
	if pw == "" || !model.StorableText(pw) {
		return "", repository.ErrInvalid
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2-sha256$600000$" + base64.RawURLEncoding.EncodeToString(salt) + "$" + base64.RawURLEncoding.EncodeToString(key), nil
}

// Only the declared v1 hash format is accepted: malformed stored input cannot
// select an unbounded iteration count or allocate an arbitrary derived key.
func CheckPassword(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(want) != 32 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, passwordIterations, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}

type loginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}
type sessionResponse struct {
	Token     string       `json:"token"`
	ExpiresAt int64        `json:"expiresAt"`
	User      userResponse `json:"user"`
}

func (s *server) authRoutes() []route {
	return []route{
		{method: "POST", path: "/console/v1/auth/login", tag: "auth", summary: "Sign in", req: loginRequest{}, resp: sessionResponse{}, handle: s.login},
		{method: "POST", path: "/console/v1/auth/logout", perm: PermAnyUser, tag: "auth", summary: "Sign out", handle: s.logout},
		{method: "GET", path: "/console/v1/user/info", perm: PermAnyUser, tag: "auth", summary: "Current user and permissions", resp: userResponse{}, handle: s.userInfo},
	}
}
func (s *server) login(c *gin.Context) {
	var req loginRequest
	if !Bind(c, &req) {
		return
	}
	u, err := s.deps.Users.GetUser(c.Request.Context(), req.Username)
	if errors.Is(err, repository.ErrNotFound) {
		CheckPassword(req.Password, dummyPasswordHash)
		Fail(c, 401, nil, model.ConsoleLoginFailed)
		return
	}
	if err != nil {
		RespondError(c, err)
		return
	}
	valid := CheckPassword(req.Password, u.Password)
	if !valid || u.Disabled || !u.Role.Valid() {
		Fail(c, 401, nil, model.ConsoleLoginFailed)
		return
	}
	// Serialize final credential validation and publication against reset
	// commit/invalidation. Password derivation stays outside the gate.
	s.credentials.RLock()
	defer s.credentials.RUnlock()
	current, err := s.deps.Users.GetUser(c.Request.Context(), u.Username)
	if errors.Is(err, repository.ErrNotFound) || err == nil && (current.Password != u.Password || current.Disabled || !current.Role.Valid()) {
		Fail(c, 401, nil, model.ConsoleLoginFailed)
		return
	}
	if err != nil {
		RespondError(c, err)
		return
	}
	u = current
	token := s.sessions.put(u.Username)
	Ok(c, sessionResponse{Token: token, ExpiresAt: s.sessions.expiresAt(token), User: toUserResponse(u)})
}
func (s *server) logout(c *gin.Context) { s.sessions.drop(bearerToken(c)); Ok(c, nil) }
