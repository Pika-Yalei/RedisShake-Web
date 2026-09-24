package main

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"golang.org/x/crypto/argon2"
)

const cookieName = "redisshake_web_session"

var adminUsernamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{2,31}$`)

func randomToken(size int) (string, error) {
	b := make([]byte, size)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func digest(value string) string {
	h := sha256.Sum256([]byte(value))
	return hex.EncodeToString(h[:])
}

func passwordHash(password string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	hash := argon2.IDKey([]byte(password), salt, 3, 64*1024, 1, 32)
	return base64.RawStdEncoding.EncodeToString(append(salt, hash...)), nil
}

func passwordMatches(password, encoded string) bool {
	data, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil || len(data) != 48 {
		return false
	}
	actual := argon2.IDKey([]byte(password), data[:16], 3, 64*1024, 1, 32)
	return subtle.ConstantTimeCompare(actual, data[16:]) == 1
}

func (s *store) hasAdmin() (bool, error) {
	var id int
	err := s.db.QueryRow("SELECT id FROM admin WHERE id=1").Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}

func (a *app) bootstrapStatus(w http.ResponseWriter, _ *http.Request) {
	exists, err := a.store.hasAdmin()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取初始化状态")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"initialized": exists})
}

func (a *app) bootstrapInit(w http.ResponseWriter, r *http.Request) {
	if origin := r.Header.Get("Origin"); origin != "" {
		parsed, err := url.Parse(origin)
		if err != nil || parsed.Host != r.Host || (parsed.Scheme != "http" && parsed.Scheme != "https") {
			writeError(w, http.StatusForbidden, "请求来源无效")
			return
		}
	}
	var input struct{ Username, Password string }
	if !readJSON(w, r, &input) {
		return
	}
	if !adminUsernamePattern.MatchString(input.Username) {
		writeError(w, http.StatusBadRequest, "账号需为 3 到 32 位，以字母开头，仅含字母、数字和下划线")
		return
	}
	if input.Password == "" || len([]rune(input.Password)) > 128 {
		writeError(w, http.StatusBadRequest, "密码不能为空且不能超过 128 个字符")
		return
	}
	hash, err := passwordHash(input.Password)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法初始化管理员")
		return
	}
	result, err := a.store.db.Exec("INSERT INTO admin(id,username,password_hash) VALUES(1,?,?)", input.Username, hash)
	if err != nil {
		writeError(w, http.StatusConflict, "管理员已初始化")
		return
	}
	if n, _ := result.RowsAffected(); n != 1 {
		writeError(w, http.StatusConflict, "管理员已初始化")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]bool{"initialized": true})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	a.authMu.Lock()
	if time.Since(a.loginWindow) > time.Minute {
		a.loginWindow = time.Now()
		a.loginFailures = 0
	}
	if a.loginFailures >= 10 {
		a.authMu.Unlock()
		writeError(w, http.StatusTooManyRequests, "登录尝试过多，请稍后再试")
		return
	}
	a.authMu.Unlock()
	var input struct{ Username, Password string }
	if !readJSON(w, r, &input) {
		return
	}
	var username, hash string
	if err := a.store.db.QueryRow("SELECT username,password_hash FROM admin WHERE id=1").Scan(&username, &hash); err != nil || subtle.ConstantTimeCompare([]byte(input.Username), []byte(username)) != 1 || !passwordMatches(input.Password, hash) {
		a.authMu.Lock()
		a.loginFailures++
		a.authMu.Unlock()
		writeError(w, http.StatusUnauthorized, "账号或密码错误")
		return
	}
	token, err := randomToken(32)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	expires := time.Now().Add(7 * 24 * time.Hour)
	if _, err := a.store.db.Exec("INSERT INTO sessions(digest,expires_at) VALUES(?,?)", digest(token), expires.Unix()); err != nil {
		writeError(w, http.StatusInternalServerError, "无法创建会话")
		return
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Value: token, Path: "/", HttpOnly: true, SameSite: http.SameSiteStrictMode, Expires: expires})
	a.authMu.Lock()
	a.loginFailures = 0
	a.authMu.Unlock()
	writeJSON(w, http.StatusOK, map[string]string{"username": username, "csrf": a.csrf(token)})
}

func (a *app) csrf(token string) string {
	h := hmac.New(sha256.New, a.store.aeadKey())
	_, _ = h.Write([]byte("csrf:" + digest(token)))
	return hex.EncodeToString(h.Sum(nil))
}

func (s *store) aeadKey() []byte {
	// This key never leaves the runner; sessions only expose its HMAC output.
	b, _ := os.ReadFile(filepath.Join(s.dir, "secret.key"))
	return b
}

func (a *app) authenticated(r *http.Request) (string, bool) {
	cookie, err := r.Cookie(cookieName)
	if err != nil || len(cookie.Value) < 32 {
		return "", false
	}
	var expiry int64
	if err := a.store.db.QueryRow("SELECT expires_at FROM sessions WHERE digest=?", digest(cookie.Value)).Scan(&expiry); err != nil || expiry <= time.Now().Unix() {
		return "", false
	}
	return cookie.Value, true
}

func (a *app) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := a.authenticated(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "请先登录")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			actual := r.Header.Get("X-CSRF-Token")
			expected := a.csrf(token)
			if subtle.ConstantTimeCompare([]byte(actual), []byte(expected)) != 1 {
				writeError(w, http.StatusForbidden, "请求校验失败")
				return
			}
		}
		next(w, r)
	}
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	token, _ := a.authenticated(r)
	var username string
	if err := a.store.db.QueryRow("SELECT username FROM admin WHERE id=1").Scan(&username); err != nil {
		writeError(w, http.StatusInternalServerError, "无法读取管理员账号")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"username": username, "csrf": a.csrf(token)})
}

func (a *app) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(cookieName)
	if cookie != nil {
		_, _ = a.store.db.Exec("DELETE FROM sessions WHERE digest=?", digest(cookie.Value))
	}
	http.SetCookie(w, &http.Cookie{Name: cookieName, Path: "/", MaxAge: -1, HttpOnly: true})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) changePassword(w http.ResponseWriter, r *http.Request) {
	var input struct{ OldPassword, NewPassword string }
	if !readJSON(w, r, &input) {
		return
	}
	if input.NewPassword == "" || len([]rune(input.NewPassword)) > 128 {
		writeError(w, http.StatusBadRequest, "新密码不能为空且不能超过 128 个字符")
		return
	}
	var oldHash string
	if err := a.store.db.QueryRow("SELECT password_hash FROM admin WHERE id=1").Scan(&oldHash); err != nil || !passwordMatches(input.OldPassword, oldHash) {
		writeError(w, http.StatusForbidden, "原密码错误")
		return
	}
	newHash, err := passwordHash(input.NewPassword)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法修改密码")
		return
	}
	tx, err := a.store.db.Begin()
	if err == nil {
		_, err = tx.Exec("UPDATE admin SET password_hash=? WHERE id=1", newHash)
		if err == nil {
			_, err = tx.Exec("DELETE FROM sessions")
		}
		if err == nil {
			err = tx.Commit()
		} else {
			_ = tx.Rollback()
		}
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "无法修改密码")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, http.StatusBadRequest, "请求格式错误")
		return false
	}
	return true
}
