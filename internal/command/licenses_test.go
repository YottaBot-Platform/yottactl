package command

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLicenseCommandsAreMounted(t *testing.T) {
	root := NewRoot()
	licenses := findSubcommand(root, "licenses")
	if licenses == nil {
		t.Fatal("licenses command missing")
	}
	for _, name := range []string{"list", "get", "status", "install", "refresh-status"} {
		if findSubcommand(licenses, name) == nil {
			t.Errorf("licenses %s command missing", name)
		}
	}
	cluster := findSubcommand(root, "cluster")
	if cluster == nil {
		t.Fatal("cluster command missing")
	}
	identity := findSubcommand(cluster, "identity")
	if identity == nil || findSubcommand(identity, "init") == nil {
		t.Fatal("cluster identity command missing")
	}
}

func TestInstallationIdentitySecretIsImmutableAndContainsEd25519Key(t *testing.T) {
	manifest, installationID, err := newInstallationIdentitySecret("yotta-bot", "yottabot-license-installation")
	if err != nil {
		t.Fatal(err)
	}
	var secret struct {
		APIVersion string            `json:"apiVersion"`
		Kind       string            `json:"kind"`
		Metadata   map[string]string `json:"metadata"`
		Immutable  bool              `json:"immutable"`
		Data       map[string]string `json:"data"`
	}
	if err := json.Unmarshal(manifest, &secret); err != nil {
		t.Fatal(err)
	}
	if secret.APIVersion != "v1" || secret.Kind != "Secret" || !secret.Immutable ||
		secret.Metadata["namespace"] != "yotta-bot" || secret.Metadata["name"] != "yottabot-license-installation" {
		t.Fatalf("secret metadata = %#v", secret)
	}
	decodedID, err := base64.StdEncoding.DecodeString(secret.Data["installation_id"])
	if err != nil || string(decodedID) != installationID || len(installationID) != 36 {
		t.Fatalf("installation id = %q, %v", decodedID, err)
	}
	keyPEM, err := base64.StdEncoding.DecodeString(secret.Data["private.key"])
	if err != nil {
		t.Fatal(err)
	}
	key, err := parsePKCS8PEMForTest(keyPEM)
	if err != nil || len(key) != ed25519.PrivateKeySize {
		t.Fatalf("private key = %d bytes, %v", len(key), err)
	}
}

func TestLicensesInstallReadsCompactArtifactFile(t *testing.T) {
	var gotMethod, gotPath string
	var gotBody map[string]string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		gotMethod, gotPath = request.Method, request.URL.Path
		body, _ := io.ReadAll(request.Body)
		_ = json.Unmarshal(body, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"license-one"}`))
	}))
	defer server.Close()
	artifactPath := filepath.Join(t.TempDir(), "license.jws")
	if err := os.WriteFile(artifactPath, []byte("header.payload.signature\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	root := NewRoot()
	root.SetArgs([]string{"--endpoint", server.URL, "--token", "test", "--output", "json", "licenses", "install", "--file", artifactPath})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	if gotMethod != http.MethodPost || gotPath != "/v1/licenses/install" || gotBody["artifact"] != "header.payload.signature" {
		t.Fatalf("request = %s %s %#v", gotMethod, gotPath, gotBody)
	}
}

func TestParseLicenseSecretRef(t *testing.T) {
	for _, test := range []struct {
		input, namespace, name, key string
	}{
		{"license", "yotta-bot", "license", "license.jws"},
		{"customer/license:entitlement.jws", "customer", "license", "entitlement.jws"},
	} {
		namespace, name, key, err := parseLicenseSecretRef(test.input)
		if err != nil || namespace != test.namespace || name != test.name || key != test.key {
			t.Fatalf("parse %q = %q %q %q, %v", test.input, namespace, name, key, err)
		}
	}
	for _, invalid := range []string{"ns/name;rm:license.jws", "Name", "name_space/license"} {
		if _, _, _, err := parseLicenseSecretRef(invalid); err == nil {
			t.Fatalf("unsafe Secret reference %q accepted", invalid)
		}
	}
}

func parsePKCS8PEMForTest(encoded []byte) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode(encoded)
	if block == nil {
		return nil, errors.New("missing PEM block")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	return parsed.(ed25519.PrivateKey), nil
}
