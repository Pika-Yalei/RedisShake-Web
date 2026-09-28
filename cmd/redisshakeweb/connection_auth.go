package main

import (
	"fmt"
	"strings"
)

const (
	authNone             = "none"
	authPassword         = "password"
	authUsername         = "username"
	authUsernamePassword = "username_password"
)

func inferAuthMode(mode, username, password string) string {
	if mode != "" {
		return mode
	}
	if username != "" {
		if password != "" {
			return authUsernamePassword
		}
		return authUsername
	}
	if password != "" {
		return authPassword
	}
	return authNone
}

func normalizeCredentials(label, mode, username, password string) (string, string, string, error) {
	mode = inferAuthMode(mode, username, password)
	switch mode {
	case authNone:
		return mode, "", "", nil
	case authPassword:
		username = ""
	case authUsername:
		password = ""
	case authUsernamePassword:
	default:
		return "", "", "", fmt.Errorf("%s 认证方式不受支持", label)
	}
	if (mode == authUsername || mode == authUsernamePassword) && strings.TrimSpace(username) == "" {
		return "", "", "", fmt.Errorf("请填写 %s 用户名", label)
	}
	if (mode == authPassword || mode == authUsernamePassword) && password == "" {
		return "", "", "", fmt.Errorf("请填写 %s 密码；不使用密码时请选择无密码的认证方式", label)
	}
	return mode, username, password, nil
}

func normalizeConnectionAuth(c Connection) (Connection, error) {
	var err error
	c.AuthMode, c.Username, c.Password, err = normalizeCredentials("Redis", c.AuthMode, c.Username, c.Password)
	if err != nil {
		return c, err
	}
	if c.Kind == "sentinel" {
		c.SentinelAuthMode, c.SentinelUsername, c.SentinelPassword, err = normalizeCredentials("Sentinel", c.SentinelAuthMode, c.SentinelUsername, c.SentinelPassword)
	} else {
		c.SentinelAuthMode, c.SentinelUsername, c.SentinelPassword = authNone, "", ""
	}
	return c, err
}

// An omitted legacy mode keeps the old blank-password-means-unchanged behavior.
// Explicit password-free modes discard saved secrets for both testing and saving.
func prepareConnection(c, old Connection) (Connection, error) {
	if c.Password == "" && (c.AuthMode == "" || c.AuthMode == authPassword || c.AuthMode == authUsernamePassword) {
		c.Password = old.Password
	}
	if c.SentinelPassword == "" && (c.SentinelAuthMode == "" || c.SentinelAuthMode == authPassword || c.SentinelAuthMode == authUsernamePassword) {
		c.SentinelPassword = old.SentinelPassword
	}
	return normalizeConnectionAuth(c)
}
