// Package config reads and writes ~/.config/yotta/config.yaml — the
// kubectl-shaped per-context config for the yottactl CLI. Plan #60
// Track B B1.
//
// Shape:
//
//	current-context: acme-prod
//	contexts:
//	  - name: acme-prod
//	    server: https://acme.yotta.bot
//	    token: <jwt-or-api-token>
//	    account-id: "100000000091"   # optional; only for multi-tenant tokens
//
// `yottactl login` (#258 Step 12c) adds the CLI session it holds at
// login.yotta.bot, and writes one context per assignment whose token it trades
// for and refreshes itself:
//
//	login:
//	  issuer: https://login.yotta.bot
//	  token: <the CLI session, 8 hours>
//	  expires-at: 2026-10-15T20:00:00Z
//	  email: pat@acme.com
//	contexts:
//	  - name: acme-prod-administrator
//	    server: https://acme.yotta.bot
//	    token: <the environment's own token, an hour at most>
//	    account-id: "100000000101"
//	    login:
//	      permission-set-id: 6f1c…
//	      token-expires-at: 2026-10-15T13:00:00Z
//
// Resolution order for the active context's server + token, highest
// first (applied by the command layer, not here):
//  1. --endpoint / --token flags
//  2. YOTTA_ENDPOINT / YOTTA_TOKEN env
//  3. the current-context entry in this file
package config

import (
	"errors"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/yaml.v3"
)

// Context is one named server+token pair (a tenant + environment).
type Context struct {
	Name      string `yaml:"name"`
	Server    string `yaml:"server"`
	Token     string `yaml:"token"`
	AccountID string `yaml:"account-id,omitempty"`
	// Login marks a context `yottactl login` wrote: its token is traded for
	// with the CLI session, not pasted, and is replaced before it expires.
	Login *LoginContext `yaml:"login,omitempty"`
}

// LoginContext is what a context `yottactl login` wrote needs to trade for
// its token: the permission set (the account is AccountID) and when the
// current token stops working.
type LoginContext struct {
	PermissionSetID string    `yaml:"permission-set-id"`
	TokenExpiresAt  time.Time `yaml:"token-expires-at,omitempty"`
}

// LoginSession is yottactl's CLI session at login.yotta.bot (#258 Step 12a):
// it lists the person's assignments and mints Access keys' ID token, nothing
// else, for eight hours.
type LoginSession struct {
	Issuer    string    `yaml:"issuer"`
	Token     string    `yaml:"token"`
	ExpiresAt time.Time `yaml:"expires-at"`
	Email     string    `yaml:"email,omitempty"`
}

// Config is the whole file.
type Config struct {
	CurrentContext string        `yaml:"current-context"`
	Contexts       []Context     `yaml:"contexts"`
	Login          *LoginSession `yaml:"login,omitempty"`
}

// Path returns the config file location: $YOTTA_CONFIG if set, else
// $XDG_CONFIG_HOME/yotta/config.yaml, else ~/.config/yotta/config.yaml.
func Path() (string, error) {
	if p := os.Getenv("YOTTA_CONFIG"); p != "" {
		return p, nil
	}
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "yotta", "config.yaml"), nil
}

// Load reads the config file. A missing file is not an error — it
// returns an empty Config so first-run `yottactl auth token` works.
func Load() (*Config, error) {
	p, err := Path()
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return &Config{}, nil
	}
	if err != nil {
		return nil, err
	}
	var c Config
	if err := yaml.Unmarshal(b, &c); err != nil {
		return nil, err
	}
	return &c, nil
}

// Save writes the config file, creating the parent directory (0700)
// and the file (0600 — it holds tokens). It writes a new file and renames it
// over the old one, so the file is never half written and is 0600 even if an
// older one was not: `yottactl login` keeps its CLI session here, and every
// command against a context it wrote may save a fresh token.
func (c *Config) Save() error {
	p, err := Path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(p), ".config-*.yaml")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp) // a no-op once renamed
	if err := f.Chmod(0o600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Current returns the current-context entry, or nil if unset/missing.
func (c *Config) Current() *Context {
	return c.find(c.CurrentContext)
}

// Find returns the named context, or nil.
func (c *Config) Find(name string) *Context {
	return c.find(name)
}

// Remove drops the named context; the current context is unset if it was that
// one.
func (c *Config) Remove(name string) {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			c.Contexts = append(c.Contexts[:i], c.Contexts[i+1:]...)
			break
		}
	}
	if c.CurrentContext == name {
		c.CurrentContext = ""
	}
}

func (c *Config) find(name string) *Context {
	for i := range c.Contexts {
		if c.Contexts[i].Name == name {
			return &c.Contexts[i]
		}
	}
	return nil
}

// SetContext upserts a context by name and makes it current.
func (c *Config) SetContext(ctx Context) {
	if existing := c.find(ctx.Name); existing != nil {
		*existing = ctx
	} else {
		c.Contexts = append(c.Contexts, ctx)
	}
	c.CurrentContext = ctx.Name
}

// UseContext switches current-context; errors if the name is unknown.
func (c *Config) UseContext(name string) error {
	if c.find(name) == nil {
		return errors.New("no context named " + name)
	}
	c.CurrentContext = name
	return nil
}
