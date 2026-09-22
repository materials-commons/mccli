package main

import (
	"context"
	"errors"
	"io"
	"os"
	"testing"

	"github.com/materials-commons/mccli/internal/config"
	"github.com/materials-commons/mccli/internal/di"
	"github.com/materials-commons/mccli/internal/mc"
)

type fakeLoginer struct {
	apiKey       string
	err          error
	loginCalls   int
	lastEmail    string
	lastPassword string
}

var _ mc.Loginer = (*fakeLoginer)(nil)

func (f *fakeLoginer) Login(username, password string) (string, error) {
	f.loginCalls++
	f.lastEmail = username
	f.lastPassword = password
	if f.err != nil {
		return "", f.err
	}
	return f.apiKey, nil
}

func withMockStdin(t *testing.T, input string, fn func()) {
	t.Helper()
	oldStdin := os.Stdin
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() failed: %v", err)
	}
	os.Stdin = r
	defer func() {
		os.Stdin = oldStdin
		_ = r.Close()
	}()

	if input != "" {
		go func() {
			defer w.Close()
			_, _ = w.WriteString(input)
		}()
	} else {
		_ = w.Close()
	}

	fn()
}

func TestLoginRunner_getRemoteClient(t *testing.T) {
	cfg := config.Global{}

	t.Run("success", func(t *testing.T) {
		loginer := &fakeLoginer{apiKey: "test-key"}
		r := loginRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return loginer, nil
				},
			},
		}

		client, err := r.getRemoteClient(cfg)
		if err != nil {
			t.Fatalf("getRemoteClient() error = %v, want nil", err)
		}
		if client != loginer {
			t.Fatalf("getRemoteClient() client = %v, want %v", client, loginer)
		}
	})

	t.Run("client creation error", func(t *testing.T) {
		expectedErr := errors.New("failed to create remote client")
		r := loginRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return nil, expectedErr
				},
			},
		}

		client, err := r.getRemoteClient(cfg)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("getRemoteClient() error = %v, want %v", err, expectedErr)
		}
		if client != nil {
			t.Fatalf("getRemoteClient() client = %v, want nil", client)
		}
	})

	t.Run("not a remote.Loginer", func(t *testing.T) {
		r := loginRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return struct{}{}, nil
				},
			},
		}

		client, err := r.getRemoteClient(cfg)
		if err == nil || err.Error() != "remote client is not a remote.Loginer" {
			t.Fatalf("getRemoteClient() error = %v, want 'remote client is not a remote.Loginer'", err)
		}
		if client != nil {
			t.Fatalf("getRemoteClient() client = %v, want nil", client)
		}
	})
}

func TestLoginRunner_getBaseRemoteClient(t *testing.T) {
	r := loginRunner{}
	client, err := r.getBaseRemoteClient("https://example.com/api")
	// di.NewBaseRemoteClient returns *mcapi.Client which does not currently implement remote.Loginer
	if err == nil || err.Error() != "remote client is not a remote.Loginer" {
		t.Fatalf("getBaseRemoteClient() error = %v, want 'remote client is not a remote.Loginer'", err)
	}
	if client != nil {
		t.Fatalf("getBaseRemoteClient() client = %v, want nil", client)
	}
}

func TestLoginRunner_promptForEmail(t *testing.T) {
	r := loginRunner{}

	t.Run("valid email without default", func(t *testing.T) {
		withMockStdin(t, "user@example.com\n", func() {
			email, err := r.promptForEmail("")
			if err != nil {
				t.Fatalf("promptForEmail() error = %v, want nil", err)
			}
			if email != "user@example.com" {
				t.Fatalf("promptForEmail() = %q, want %q", email, "user@example.com")
			}
		})
	})

	t.Run("valid email with surrounding whitespace", func(t *testing.T) {
		withMockStdin(t, "   user@example.com   \n", func() {
			email, err := r.promptForEmail("default@example.com")
			if err != nil {
				t.Fatalf("promptForEmail() error = %v, want nil", err)
			}
			if email != "user@example.com" {
				t.Fatalf("promptForEmail() = %q, want %q", email, "user@example.com")
			}
		})
	})

	t.Run("empty email with default", func(t *testing.T) {
		withMockStdin(t, "\n", func() {
			email, err := r.promptForEmail("default@example.com")
			if err != nil {
				t.Fatalf("promptForEmail() error = %v, want nil", err)
			}
			if email != "default@example.com" {
				t.Fatalf("promptForEmail() = %q, want %q", email, "default@example.com")
			}
		})
	})

	t.Run("empty email without default", func(t *testing.T) {
		withMockStdin(t, "\n", func() {
			email, err := r.promptForEmail("")
			if err == nil || err.Error() != "empty email" {
				t.Fatalf("promptForEmail() error = %v, want 'empty email'", err)
			}
			if email != "" {
				t.Fatalf("promptForEmail() = %q, want empty string", email)
			}
		})
	})

	t.Run("read error or EOF", func(t *testing.T) {
		withMockStdin(t, "", func() {
			email, err := r.promptForEmail("")
			if !errors.Is(err, io.EOF) {
				t.Fatalf("promptForEmail() error = %v, want io.EOF", err)
			}
			if email != "" {
				t.Fatalf("promptForEmail() = %q, want empty string", email)
			}
		})
	})
}

func TestLoginRunner_promptForPassword(t *testing.T) {
	r := loginRunner{}

	t.Run("non-terminal stdin returns error", func(t *testing.T) {
		withMockStdin(t, "mypassword\n", func() {
			pw, err := r.promptForPassword("user@example.com")
			if err == nil {
				t.Fatalf("promptForPassword() error = nil, want error for non-terminal stdin")
			}
			if pw != "" {
				t.Fatalf("promptForPassword() = %q, want empty string", pw)
			}
		})
	})
}

func TestLoginRunner_setupGlobalConfig(t *testing.T) {
	r := loginRunner{}
	ctx := context.Background()
	opts := loginOpts{MCAPIUrl: "https://example.com/api"}

	t.Run("getBaseRemoteClient error propagates", func(t *testing.T) {
		err := r.setupGlobalConfig(ctx, opts)
		if err == nil || err.Error() != "remote client is not a remote.Loginer" {
			t.Fatalf("setupGlobalConfig() error = %v, want 'remote client is not a remote.Loginer'", err)
		}
	})
}

func TestLoginRunner_loginToExisting(t *testing.T) {
	ctx := context.Background()
	opts := loginOpts{MCAPIUrl: "https://example.com/api"}
	globalCfg := config.Global{
		DefaultRemote: config.Remote{
			Email: "user@example.com",
			MCURL: "https://example.com/api",
		},
	}

	t.Run("getRemoteClient error", func(t *testing.T) {
		expectedErr := errors.New("remote client error")
		r := loginRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return nil, expectedErr
				},
			},
		}

		err := r.loginToExisting(ctx, globalCfg, opts)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("loginToExisting() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("promptForPassword returns empty or error on non-terminal stdin", func(t *testing.T) {
		loginer := &fakeLoginer{apiKey: "apikey-123"}
		r := loginRunner{
			deps: di.Dependencies{
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return loginer, nil
				},
			},
		}

		withMockStdin(t, "password\n", func() {
			err := r.loginToExisting(ctx, globalCfg, opts)
			if err != nil {
				t.Fatalf("loginToExisting() error = %v, want nil", err)
			}
		})
	})
}

func TestLoginRunner_Run(t *testing.T) {
	ctx := context.Background()
	opts := loginOpts{MCAPIUrl: "https://example.com/api"}

	t.Run("LoadGlobal error calls setupGlobalConfig which propagates getBaseRemoteClient error", func(t *testing.T) {
		r := loginRunner{
			deps: di.Dependencies{
				LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
					return config.Global{}, errors.New("config not found")
				},
			},
		}

		err := r.Run(ctx, opts)
		if err == nil || err.Error() != "remote client is not a remote.Loginer" {
			t.Fatalf("Run() error = %v, want 'remote client is not a remote.Loginer'", err)
		}
	})

	t.Run("LoadGlobal success and loginToExisting getRemoteClient error", func(t *testing.T) {
		expectedErr := errors.New("client error")
		r := loginRunner{
			deps: di.Dependencies{
				LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
					return config.Global{
						DefaultRemote: config.Remote{
							Email: "user@example.com",
						},
					}, nil
				},
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return nil, expectedErr
				},
			},
		}

		err := r.Run(ctx, opts)
		if !errors.Is(err, expectedErr) {
			t.Fatalf("Run() error = %v, want %v", err, expectedErr)
		}
	})

	t.Run("LoadGlobal success and loginToExisting success", func(t *testing.T) {
		loginer := &fakeLoginer{apiKey: "apikey-123"}
		r := loginRunner{
			deps: di.Dependencies{
				LoadGlobal: func(ctx context.Context, path string) (config.Global, error) {
					return config.Global{
						DefaultRemote: config.Remote{
							Email: "user@example.com",
						},
					}, nil
				},
				NewDefaultRemoteClient: func(global config.Global) (di.RemoteClient, error) {
					return loginer, nil
				},
			},
		}

		withMockStdin(t, "password\n", func() {
			err := r.Run(ctx, opts)
			if err != nil {
				t.Fatalf("Run() error = %v, want nil", err)
			}
		})
	})
}

func TestRunLoginCmd_Production(t *testing.T) {
	ctx := context.Background()
	t.Setenv("HOME", t.TempDir())

	opts := loginOpts{MCAPIUrl: "https://example.com/api"}
	err := runLoginCmd(ctx, opts)
	if err == nil || err.Error() != "remote client is not a remote.Loginer" {
		t.Fatalf("RunLoginCmd() error = %v, want 'remote client is not a remote.Loginer'", err)
	}
}
