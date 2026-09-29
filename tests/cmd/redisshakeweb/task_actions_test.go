package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestTaskPreviewDoesNotSaveAndRemovedActionsAreUnavailable(t *testing.T) {
	s, err := openStore(t.TempDir())
	require.NoError(t, err)
	defer s.db.Close()
	a := &app{store: s}
	handler := a.routes()
	token := strings.Repeat("test-session", 4)
	_, err = s.db.Exec("INSERT INTO sessions(digest,expires_at) VALUES(?,?)", digest(token), time.Now().Add(time.Hour).Unix())
	require.NoError(t, err)
	request := func(method, path, body string, authenticated bool) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		if authenticated {
			req.AddCookie(&http.Cookie{Name: cookieName, Value: token})
			req.Header.Set("X-CSRF-Token", a.csrf(token))
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)
		return w
	}
	for _, c := range []Connection{{ID: "source", Name: "Source", Kind: "standalone", Address: "source.invalid:6379"}, {ID: "target", Name: "Target", Kind: "standalone", Address: "target.invalid:6379"}} {
		require.NoError(t, s.saveConnection(c))
	}
	original := Task{ID: "existing", Name: "Immutable task", SourceID: "source", TargetID: "target", DBMap: map[string]int{"0": 0}, TargetPolicy: "require_empty"}
	require.NoError(t, s.saveTask(original))
	// Invalid DB mapping is checked without contacting any Redis. Even a supplied
	// existing ID and changed name must not update the stored task during preview.
	draft := `{"id":"existing","name":"Changed draft","sourceId":"source","targetId":"target","dbMap":{},"targetPolicy":"require_empty"}`
	require.Equal(t, http.StatusUnauthorized, request(http.MethodPost, "/api/tasks/preflight", draft, false).Code)
	for range 2 {
		w := request(http.MethodPost, "/api/tasks/preflight", draft, true)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var checks []Check
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &checks))
		require.Len(t, checks, 1)
		require.False(t, checks[0].OK)
		require.Contains(t, checks[0].Message, "至少选择一个源 DB")
	}
	for _, action := range []struct {
		method, path string
		status       int
	}{
		{http.MethodPut, "/api/tasks/existing", http.StatusMethodNotAllowed},
		{http.MethodPost, "/api/tasks/existing/clear-preview", http.StatusNotFound},
		{http.MethodPost, "/api/tasks/existing/clear-confirm", http.StatusNotFound},
	} {
		w := request(action.method, action.path, draft, true)
		require.Equal(t, action.status, w.Code, action.path)
	}
	tasks, err := s.tasks()
	require.NoError(t, err)
	require.Len(t, tasks, 1)
	require.Equal(t, original.Name, tasks[0].Name)
	require.Equal(t, original.DBMap, tasks[0].DBMap)
	require.Equal(t, http.StatusOK, request(http.MethodGet, "/api/tasks/existing", "", true).Code)
}
