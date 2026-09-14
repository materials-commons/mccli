package cmds

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/google/uuid"
	"github.com/materials-commons/mccli/pkg/config"
	"github.com/materials-commons/mccli/pkg/di"
	"github.com/materials-commons/mccli/pkg/remote"
	"golang.org/x/term"
)

type loginRunner struct {
	deps di.Dependencies
}

type LoginOpts struct {
	MCAPIUrl string
}

func RunLoginCmd(ctx context.Context, opts LoginOpts) error {
	return loginRunner{deps: di.Production()}.Run(ctx, opts)
}

func (r loginRunner) Run(ctx context.Context, opts LoginOpts) error {
	globalConfig, err := r.deps.LoadGlobal(ctx, "")
	if err != nil {
		if err := r.setupGlobalConfig(ctx, opts); err != nil {
			return err
		}
	} else {
		return r.loginToExisting(ctx, globalConfig, opts)
	}

	return nil
}

func (r loginRunner) getRemoteClient(cfg config.Global) (remote.Loginer, error) {
	remoteAny, err := r.deps.NewDefaultRemoteClient(cfg)
	if err != nil {
		return nil, err
	}

	remoteClient, ok := remoteAny.(remote.Loginer)
	if !ok {
		return nil, errors.New("remote client is not a remote.Loginer")
	}

	return remoteClient, nil
}

func (r loginRunner) getBaseRemoteClient(mcapiURL string) (remote.Loginer, error) {
	remoteAny := di.NewBaseRemoteClient(mcapiURL)

	remoteClient, ok := remoteAny.(remote.Loginer)
	if !ok {
		return nil, errors.New("remote client is not a remote.Loginer")
	}

	return remoteClient, nil
}

// setupGlobalConfig sets up the global configuration for the login command. Since nothing
// is available yet, it needs to prompt for the email address and the password.
func (r loginRunner) setupGlobalConfig(ctx context.Context, opts LoginOpts) error {
	var (
		email    string
		password string
		err      error
	)
	remoteClient, err := r.getBaseRemoteClient(opts.MCAPIUrl)
	if err != nil {
		return err
	}

	for {
		email, err = r.promptForEmail(email)
		if err != nil {
			return err
		}
		if email != "" {
			break
		}

		password, err = r.promptForPassword(email)
		if err != nil {
			return err
		}
		if password != "" {
			break
		}

		apikey, err := remoteClient.Login(email, password)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "Password is incorrect. Please try again.")
			continue
		}

		globalConfig := config.Global{
			DefaultRemote: config.Remote{
				Email:  email,
				APIKey: apikey,
				MCURL:  opts.MCAPIUrl,
			},

			Remotes: []config.Remote{
				{
					Email:  email,
					APIKey: apikey,
					MCURL:  opts.MCAPIUrl,
				},
			},

			ClientUUID: uuid.NewString(),
		}
		globalConfig.DefaultRemote.APIKey = apikey
		if err := config.SaveGlobal(ctx, globalConfig, ""); err != nil {
			return err
		}

		return nil
	}

	//email, err := r.promptForEmail()
	return nil
}

func (r loginRunner) loginToExisting(ctx context.Context, globalConfig config.Global, opts LoginOpts) error {
	remoteClient, err := r.getRemoteClient(globalConfig)
	if err != nil {
		return err
	}

	email := globalConfig.DefaultRemote.Email

	for {
		password, err := r.promptForPassword(email)
		if password == "" {
			return nil
		}

		apikey, err := remoteClient.Login(email, password)
		if err != nil {
			_, _ = fmt.Fprintln(os.Stderr, "Password is incorrect. Please try again.")
			continue
		}

		globalConfig.DefaultRemote.APIKey = apikey
		if err := config.SaveGlobal(ctx, globalConfig, ""); err != nil {
			return err
		}

		return nil
	}
}

func (r loginRunner) promptForPassword(email string) (string, error) {
	for {
		_, _ = fmt.Fprintf(os.Stderr, "Password for %s: ", email)

		passwordBytes, err := term.ReadPassword(int(os.Stdin.Fd()))
		_, _ = fmt.Fprintln(os.Stderr)

		if err != nil {
			return "", err
		}

		password := strings.TrimSpace(string(passwordBytes))
		if password == "" {
			return "", errors.New("empty password")
		}

		return password, nil
	}
}

// promptForEmail will prompt a user for their email address. If a defaultEmail is
// supplied, then use that as the default value, but give the user a chance to
// change it.
func (r loginRunner) promptForEmail(defaultEmail string) (string, error) {
	if defaultEmail != "" {
		// Show user the default email address
		_, _ = fmt.Fprintf(os.Stderr, "Enter email address (%s): ", defaultEmail)
	} else {
		// No default email address supplied.
		_, _ = fmt.Fprintf(os.Stderr, "Enter email address: ")
	}

	// read the email address from the user.
	reader := bufio.NewReader(os.Stdin)
	email, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}

	email = strings.TrimSpace(email)

	// if email is empty, then return the defaultEmail if it is supplied, otherwise
	// return blank and an error.
	if email == "" {
		if defaultEmail != "" {
			// email is blank, but defaultEmail is supplied, so return it.
			return defaultEmail, nil
		}
		// email is blank, no default, so return an error.
		return "", errors.New("empty email")
	}

	// email is not blank, so return it.
	return email, nil
}
