package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestInitializeAdministratorAccount(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.db.Close()
	handler := (&app{store: st}).routes()
	request := func(method, path, body string) *httptest.ResponseRecorder {
		t.Helper()
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(method, path, strings.NewReader(body)))
		return w
	}
	if w := request(http.MethodPost, "/api/password/reset", `{"code":"old-code","newPassword":"new-password"}`); w.Code != http.StatusNotFound {
		t.Fatalf("removed password reset endpoint: %d %s", w.Code, w.Body.String())
	}
	crossOrigin := httptest.NewRequest(http.MethodPost, "/api/bootstrap/init", strings.NewReader(`{"username":"attacker","password":"valid-password-123"}`))
	crossOrigin.Header.Set("Origin", "https://other.example")
	crossOriginResult := httptest.NewRecorder()
	handler.ServeHTTP(crossOriginResult, crossOrigin)
	if crossOriginResult.Code != http.StatusForbidden {
		t.Fatalf("cross-origin initialization: %d", crossOriginResult.Code)
	}
	if w := request(http.MethodPost, "/api/bootstrap/init", `{"username":"1invalid","password":"valid-password-123"}`); w.Code != http.StatusBadRequest {
		t.Fatalf("invalid account: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/bootstrap/init", `{"username":"audit_admin","password":""}`); w.Code != http.StatusBadRequest {
		t.Fatalf("empty password: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/bootstrap/init", `{"username":"audit_admin","password":"short"}`); w.Code != http.StatusCreated {
		t.Fatalf("initialize: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/bootstrap/init", `{"username":"second_admin","password":"valid-password-123"}`); w.Code != http.StatusConflict {
		t.Fatalf("second initialization: %d %s", w.Code, w.Body.String())
	}
	if w := request(http.MethodPost, "/api/login", `{"username":"admin","password":"valid-password-123"}`); w.Code != http.StatusUnauthorized {
		t.Fatalf("default account should not authenticate: %d", w.Code)
	}
	w := request(http.MethodPost, "/api/login", `{"username":"audit_admin","password":"short"}`)
	if w.Code != http.StatusOK {
		t.Fatalf("custom account login: %d %s", w.Code, w.Body.String())
	}
	var loggedIn map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &loggedIn); err != nil || loggedIn["username"] != "audit_admin" {
		t.Fatalf("login returned wrong account: %s, %v", w.Body.String(), err)
	}
	me := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
	for _, cookie := range w.Result().Cookies() {
		req.AddCookie(cookie)
	}
	handler.ServeHTTP(me, req)
	if me.Code != http.StatusOK || !strings.Contains(me.Body.String(), `"username":"audit_admin"`) {
		t.Fatalf("current session: %d %s", me.Code, me.Body.String())
	}
}

func TestExistingAdministratorDefaultsToAdmin(t *testing.T) {
	dir := t.TempDir()
	db, err := sql.Open("sqlite", filepath.Join(dir, "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	hash, err := passwordHash("existing-password-123")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("CREATE TABLE admin (id INTEGER PRIMARY KEY CHECK(id=1), password_hash BLOB NOT NULL)"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO admin(id,password_hash) VALUES(1,?)", hash); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	st, err := openStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer st.db.Close()
	var username string
	if err := st.db.QueryRow("SELECT username FROM admin WHERE id=1").Scan(&username); err != nil || username != "admin" {
		t.Fatalf("legacy admin migration: %q, %v", username, err)
	}
	w := httptest.NewRecorder()
	(&app{store: st}).routes().ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/api/login", strings.NewReader(`{"username":"admin","password":"existing-password-123"}`)))
	if w.Code != http.StatusOK {
		t.Fatalf("legacy admin login: %d %s", w.Code, w.Body.String())
	}
}
