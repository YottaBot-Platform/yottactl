package command

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
)

var kubernetesName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
var kubernetesNamespace = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
var kubernetesResourceName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]*[a-z0-9])?$`)

// newClusterCmd exposes the public half of the installation identity as an
// activation request. The service reads the live kube-system namespace UID and
// the operator-owned installation Secret; yottactl never reads or prints the
// installation private key.
func newClusterCmd(env *Env) *cobra.Command {
	cluster := &cobra.Command{
		Use:     "cluster",
		Short:   "Cluster identity and activation operations",
		GroupID: groupID("Account"),
	}
	identity := &cobra.Command{
		Use:   "identity",
		Short: "Print this cluster's license activation request (public material only)",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			return doCall(env, "GET", "/v1/licenses/activation-request", nil)
		},
	}
	identity.AddCommand(newClusterIdentityInitCmd())
	cluster.AddCommand(identity)
	return cluster
}

func newClusterIdentityInitCmd() *cobra.Command {
	var namespace, secretName string
	command := &cobra.Command{
		Use:   "init",
		Short: "Create this cluster's immutable installation identity Secret",
		Args:  cobra.NoArgs,
		RunE: func(command *cobra.Command, _ []string) error {
			if !validKubernetesNamespace(namespace) || !validKubernetesResourceName(secretName) {
				return &ExitError{Code: 4, Msg: "invalid namespace or Secret name"}
			}
			manifest, installationID, err := newInstallationIdentitySecret(namespace, secretName)
			if err != nil {
				return &ExitError{Code: 1, Msg: "generate installation identity: " + err.Error()}
			}
			kubectl := exec.CommandContext(command.Context(), "kubectl", "create", "--filename", "-")
			kubectl.Stdin = strings.NewReader(string(manifest))
			output, err := kubectl.CombinedOutput()
			if err != nil {
				return &ExitError{Code: 1, Msg: "create installation identity Secret: " + strings.TrimSpace(string(output))}
			}
			_, _ = fmt.Fprintf(command.OutOrStdout(), "Created immutable Secret %s/%s for installation %s\n", namespace, secretName, installationID)
			return nil
		},
	}
	command.Flags().StringVar(&namespace, "namespace", "yotta-bot", "Kubernetes namespace for the installation identity Secret")
	command.Flags().StringVar(&secretName, "secret", "yottabot-license-installation", "installation identity Secret name")
	return command
}

func newInstallationIdentitySecret(namespace, name string) ([]byte, string, error) {
	_, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, "", err
	}
	encodedKey, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return nil, "", err
	}
	installationID, err := randomUUID()
	if err != nil {
		return nil, "", err
	}
	manifest := struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Metadata   map[string]string `json:"metadata"`
		Immutable  bool              `json:"immutable"`
		Type       string            `json:"type"`
		Data       map[string]string `json:"data"`
	}{
		APIVersion: "v1", Kind: "Secret", Metadata: map[string]string{"name": name, "namespace": namespace},
		Immutable: true, Type: "Opaque", Data: map[string]string{
			"installation_id": base64.StdEncoding.EncodeToString([]byte(installationID)),
			"private.key":     base64.StdEncoding.EncodeToString(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: encodedKey})),
		},
	}
	encoded, err := json.Marshal(manifest)
	return encoded, installationID, err
}

func randomUUID() (string, error) {
	value := make([]byte, 16)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	encoded := hex.EncodeToString(value)
	return encoded[0:8] + "-" + encoded[8:12] + "-" + encoded[12:16] + "-" + encoded[16:20] + "-" + encoded[20:32], nil
}

func newLicensesInstallCmd(env *Env) *cobra.Command {
	var artifactFile, secretRef, keyBundleFile string
	command := &cobra.Command{
		Use:   "install (--file <license.jws> | --from-secret [namespace/]name[:key])",
		Short: "Verify and install a signed license artifact",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			if (artifactFile == "") == (secretRef == "") {
				return &ExitError{Code: 4, Msg: "exactly one of --file or --from-secret is required"}
			}
			var artifact string
			var err error
			if artifactFile != "" {
				artifact, err = readCompactFile(artifactFile)
			} else {
				artifact, err = readLicenseSecret(secretRef)
			}
			if err != nil {
				return &ExitError{Code: 4, Msg: err.Error()}
			}
			body := map[string]any{"artifact": artifact}
			if keyBundleFile != "" {
				bundle, err := readCompactFile(keyBundleFile)
				if err != nil {
					return &ExitError{Code: 4, Msg: err.Error()}
				}
				body["key_bundle"] = bundle
			}
			return doCall(env, "POST", "/v1/licenses/install", body)
		},
	}
	command.Flags().StringVarP(&artifactFile, "file", "f", "", "compact JWS license file")
	command.Flags().StringVar(&secretRef, "from-secret", "", "Kubernetes Secret reference [namespace/]name[:key] (default namespace yotta-bot, key license.jws)")
	command.Flags().StringVar(&keyBundleFile, "key-bundle", "", "optional root-signed verification-bundle JWS file")
	return command
}

func replaceLicensesInstall(env *Env, licenses *cobra.Command) {
	for _, sub := range licenses.Commands() {
		if sub.Name() == "install" {
			licenses.RemoveCommand(sub)
			break
		}
	}
	licenses.AddCommand(newLicensesInstallCmd(env))
}

func readCompactFile(path string) (string, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	compact := strings.TrimSpace(string(contents))
	if compact == "" || strings.ContainsAny(compact, "\r\n\t ") {
		return "", fmt.Errorf("%s does not contain one compact JWS", path)
	}
	return compact, nil
}

func readLicenseSecret(reference string) (string, error) {
	namespace, name, key, err := parseLicenseSecretRef(reference)
	if err != nil {
		return "", err
	}
	// jsonpath quotes are kept out of user input: name/namespace/key are
	// validated above, and exec.Command passes every argument without a shell.
	jsonPath := fmt.Sprintf(`{.data.%s}`, strings.ReplaceAll(key, ".", `\.`))
	output, err := exec.Command("kubectl", "get", "secret", name, "--namespace", namespace, "--output", "jsonpath="+jsonPath).Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return "", fmt.Errorf("read Kubernetes Secret %s/%s: %s", namespace, name, strings.TrimSpace(string(exitErr.Stderr)))
		}
		return "", fmt.Errorf("read Kubernetes Secret %s/%s: %w", namespace, name, err)
	}
	decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(output)))
	if err != nil {
		return "", fmt.Errorf("decode Kubernetes Secret %s/%s key %s: %w", namespace, name, key, err)
	}
	compact := strings.TrimSpace(string(decoded))
	if compact == "" || strings.ContainsAny(compact, "\r\n\t ") {
		return "", fmt.Errorf("Kubernetes Secret %s/%s key %s does not contain one compact JWS", namespace, name, key)
	}
	return compact, nil
}

func parseLicenseSecretRef(reference string) (namespace, name, key string, err error) {
	namespace, key = "yotta-bot", "license.jws"
	namePart := strings.TrimSpace(reference)
	if before, after, found := strings.Cut(namePart, ":"); found {
		namePart, key = before, after
	}
	if before, after, found := strings.Cut(namePart, "/"); found {
		namespace, name = before, after
	} else {
		name = namePart
	}
	if !validKubernetesNamespace(namespace) || !validKubernetesResourceName(name) || len(key) > 253 || !kubernetesName.MatchString(key) {
		return "", "", "", errors.New("invalid Secret reference; use [namespace/]name[:key]")
	}
	return namespace, name, key, nil
}

func validKubernetesNamespace(value string) bool {
	return len(value) <= 63 && kubernetesNamespace.MatchString(value)
}

func validKubernetesResourceName(value string) bool {
	return len(value) <= 253 && kubernetesResourceName.MatchString(value)
}
