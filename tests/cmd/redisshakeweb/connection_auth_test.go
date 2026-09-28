package main

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestConnectionAuthModes(t *testing.T) {
	for _, tc := range []struct {
		mode, user, password, wantUser, wantPassword string
		invalid                                      bool
	}{
		{authNone, "stale-user", "stale-secret", "", "", false},
		{authPassword, "stale-user", "secret", "", "secret", false},
		{authUsername, "reader", "stale-secret", "reader", "", false},
		{authUsernamePassword, "reader", "secret", "reader", "secret", false},
		{"", "reader", "", "reader", "", false},
		{"", "", "secret", "", "secret", false},
		{authPassword, "", "", "", "", true},
		{authUsername, " ", "", "", "", true},
		{authUsernamePassword, "reader", "", "", "", true},
		{"unknown", "", "", "", "", true},
	} {
		t.Run(tc.mode+"/"+tc.user+"/"+tc.password, func(t *testing.T) {
			c, err := normalizeConnectionAuth(Connection{Kind: "standalone", AuthMode: tc.mode, Username: tc.user, Password: tc.password})
			if (err != nil) != tc.invalid {
				t.Fatalf("unexpected validation error: %v", err)
			}
			if !tc.invalid && (c.Username != tc.wantUser || c.Password != tc.wantPassword) {
				t.Fatal("wrong effective credentials")
			}
		})
	}
}

func TestConnectionAPIKeepsOrClearsSavedPasswords(t *testing.T) {
	st, err := openStore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer st.db.Close()
	a := &app{store: st}
	old := Connection{ID: "saved", Name: "saved", Kind: "sentinel", SentinelAddress: "localhost:26379", SentinelMaster: "primary", Username: "redis-user", Password: "redis-secret", SentinelUsername: "sentinel-user", SentinelPassword: "sentinel-secret"}
	if err := st.saveConnection(old); err != nil {
		t.Fatal(err)
	}
	listed, err := st.connections()
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 1 || listed[0].AuthMode != authUsernamePassword || !listed[0].HasPassword || !listed[0].HasSentinelPassword {
		t.Fatal("legacy connection was not inferred before redaction")
	}
	for _, tc := range []struct{ name, mode, sentinelMode, password, sentinelPassword string }{
		{"keep", authUsernamePassword, authUsernamePassword, "", ""},
		{"replace", authUsernamePassword, authUsernamePassword, "replacement", "sentinel-replacement"},
		{"username only", authUsername, authUsername, "", ""},
		{"clear", authNone, authNone, "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := st.saveConnection(old); err != nil {
				t.Fatal(err)
			}
			draft := old
			draft.AuthMode = tc.mode
			draft.SentinelAuthMode = tc.sentinelMode
			draft.Password = tc.password
			draft.SentinelPassword = tc.sentinelPassword
			body, _ := json.Marshal(draft)
			req := httptest.NewRequest("PUT", "/api/connections/saved", strings.NewReader(string(body)))
			req.SetPathValue("id", "saved")
			w := httptest.NewRecorder()
			a.updateConnection(w, req)
			if w.Code != 200 {
				t.Fatalf("update: %d %s", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "secret") || strings.Contains(w.Body.String(), "replacement") {
				t.Fatal("saved password leaked")
			}
			saved, err := st.connection(old.ID)
			if err != nil {
				t.Fatal(err)
			}
			wantUser, wantPassword, wantSentinelPassword := "redis-user", "redis-secret", "sentinel-secret"
			if tc.name == "replace" {
				wantPassword, wantSentinelPassword = tc.password, tc.sentinelPassword
			}
			if tc.name == "username only" || tc.name == "clear" {
				wantPassword, wantSentinelPassword = "", ""
			}
			if tc.name == "clear" {
				wantUser = ""
				if saved.SentinelUsername != "" {
					t.Fatal("Sentinel username not cleared")
				}
			}
			if saved.Username != wantUser || saved.Password != wantPassword || saved.SentinelPassword != wantSentinelPassword {
				t.Fatal("credentials were not preserved/replaced/cleared as requested")
			}
		})
	}
}

func TestConnectionAuthTestAndSaveSharePasswordSemantics(t *testing.T) {
	old := Connection{Kind: "sentinel", Password: "old", SentinelPassword: "old-sentinel"}
	for _, mode := range []string{authNone, authUsername} {
		c, err := prepareConnection(Connection{Kind: "sentinel", AuthMode: mode, Username: "redis-user", SentinelAuthMode: mode, SentinelUsername: "sentinel-user"}, old)
		if err != nil {
			t.Fatal(err)
		}
		if c.Password != "" || c.SentinelPassword != "" {
			t.Fatal("password-free mode inherited saved passwords")
		}
	}
	legacy, err := prepareConnection(Connection{Kind: "sentinel"}, old)
	if err != nil || legacy.Password != old.Password || legacy.SentinelPassword != old.SentinelPassword {
		t.Fatalf("legacy edit did not preserve passwords: %v", err)
	}
}

func TestGeneratedConfigHonorsExplicitAuthModes(t *testing.T) {
	source := Connection{ID: "source", Kind: "sentinel", AuthMode: authUsername, Username: "reader", Password: "stale-redis-secret", SentinelMaster: "primary", SentinelAddress: "localhost:26379", SentinelAuthMode: authNone, SentinelUsername: "stale-user", SentinelPassword: "stale-sentinel-secret"}
	target := Connection{ID: "target", Kind: "standalone", AuthMode: authNone, Address: "localhost:6380", Password: "stale-target-secret"}
	task := Task{Name: "auth", SourceID: source.ID, TargetID: target.ID, DBMap: map[string]int{"0": 1}, TargetPolicy: "overwrite"}
	config, err := generateConfig(task, source, target, t.TempDir(), false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(config, "stale-") {
		t.Fatal("disabled credentials reached the migration config")
	}
	if !strings.Contains(config, "username = \"reader\"\npassword = \"\"") {
		t.Fatal("named empty-password auth missing from migration config")
	}
}
