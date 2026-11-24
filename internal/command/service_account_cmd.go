package command

// `yottactl org service-accounts keygen` and `yottactl login
// --service-account` (plan 334 step 6).

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/YottaBot-Platform/yottactl/internal/config"
	"github.com/spf13/cobra"
)

func newOrgServiceAccountsCmd() *cobra.Command {
	sa := &cobra.Command{Use: "service-accounts", Short: "Make a key for one of your organization's service accounts"}
	var out string
	keygen := &cobra.Command{
		Use:   "keygen",
		Short: "Make an Ed25519 key pair: the private key to a new file, its public key to paste into Identity",
		Long: `Make an Ed25519 key pair for a service account.

The private key goes, in PKCS #8 PEM, to --out: a new file only you can read; a
file that exists is refused. It never leaves this computer. The public key and
its key ID are printed: paste the public key into the service account's Keys
in Identity (Create key, Paste a public key), and leave Key ID empty there, so
the ID is the key's own thumbprint, which yottactl works out from the file.

Then sign in with it:
  yottactl login --service-account sa-<id> --key <file>

Example:
  yottactl org service-accounts keygen --out ./terraform.pem`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if out == "" {
				return &ExitError{Code: 2, Msg: "--out names the new private key's file"}
			}
			pub, priv, err := ed25519.GenerateKey(rand.Reader)
			if err != nil {
				return err
			}
			der, err := x509.MarshalPKCS8PrivateKey(priv)
			if err != nil {
				return err
			}
			f, err := os.OpenFile(out, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
			if err != nil {
				if errors.Is(err, os.ErrExist) {
					return &ExitError{Code: 2, Msg: out + " exists: name a new file"}
				}
				return &ExitError{Code: 1, Msg: err.Error()}
			}
			if err := pem.Encode(f, &pem.Block{Type: "PRIVATE KEY", Bytes: der}); err != nil {
				f.Close()
				return err
			}
			if err := f.Close(); err != nil {
				return err
			}
			b, _ := json.Marshal(map[string]string{"public_key": base64.RawURLEncoding.EncodeToString(pub), "key_id": keyThumbprint(pub), "private_key_file": out})
			fmt.Fprintln(cmd.OutOrStdout(), string(b))
			fmt.Fprintln(cmd.ErrOrStderr(), "Paste public_key into the service account's Keys in Identity; leave its Key ID empty.")
			return nil
		},
	}
	keygen.Flags().StringVar(&out, "out", "", "the new private key's file (made only you can read; an existing file is refused)")
	sa.AddCommand(keygen)
	return sa
}

// runServiceAccountLogin checks the service account can sign in with key,
// then keeps where the key is. A person's live sign-in is left alone, and
// refused: one way at a time.
func runServiceAccountLogin(cmd *cobra.Command, issuerFlag, account, keyFile, keyID string) error {
	cfg, err := config.Load()
	if err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	if cfg.Login != nil && cfg.Login.Token != "" && cfg.Login.ExpiresAt.After(loginNow()) {
		return &ExitError{Code: 2, Msg: "you're signed in as " + firstNonEmpty(cfg.Login.Email, "a person") + ": run `yottactl logout` first"}
	}
	clientID, err := serviceAccountClientID(account)
	if err != nil {
		return err
	}
	if keyFile == "" {
		return &ExitError{Code: 2, Msg: "--key names the service account's private key file (`yottactl org service-accounts keygen` makes one)"}
	}
	abs, err := filepath.Abs(keyFile)
	if err != nil {
		return &ExitError{Code: 2, Msg: err.Error()}
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		return &ExitError{Code: 2, Msg: "read the key: " + err.Error()}
	}
	key, err := parsePrivateKey(raw)
	if err != nil {
		return &ExitError{Code: 2, Msg: abs + ": " + err.Error()}
	}
	issuer, ok := httpsOrigin(firstNonEmpty(issuerFlag, cfg.LoginIssuer, defaultLoginIssuer))
	if !ok {
		return &ExitError{Code: 2, Msg: "--issuer must be an https origin, like " + defaultLoginIssuer}
	}
	c := &machineCredential{issuer: issuer, clientID: clientID, keyID: keyID, key: key}
	if _, err := c.token(c.audienceFor("/v1/directory")); err != nil {
		return err
	}
	cfg.ServiceAccount = &config.ServiceAccountLogin{Issuer: issuer, ClientID: clientID, KeyFile: abs, KeyID: keyID}
	cfg.Login, cfg.LoginIssuer = nil, issuer
	if err := cfg.Save(); err != nil {
		return &ExitError{Code: 1, Msg: err.Error()}
	}
	// The token it was just issued serves the next command.
	c.saved = true
	audience := c.audienceFor("/v1/directory")
	c.keep(audience, machineTokens[clientID+" "+audience])
	fmt.Fprintf(cmd.ErrOrStderr(), "Signed in at %s as %s. `yottactl org` commands act as it, with its roles; `yottactl logout` forgets it.\n", issuer, clientID)
	return nil
}
