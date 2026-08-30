package crypto_test

import (
	"crypto/x509"
	"encoding/pem"
	"os"
	"testing"

	"github.com/gotd/td/telegram"
	tlrpccrypto "github.com/r6m/tlrpc/crypto"
)

func TestServerKeyFingerprintMatchesGotd(t *testing.T) {
	key, err := tlrpccrypto.GenerateServerKey()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	want := (telegram.PublicKey{RSA: &key.Key.PublicKey}).Fingerprint()
	if key.ID != want {
		t.Fatalf("fingerprint = %d, gotd = %d", key.ID, want)
	}
	if got := tlrpccrypto.PublicKeyFingerprint(&key.Key.PublicKey); got != want {
		t.Fatalf("public fingerprint = %d, gotd = %d", got, want)
	}
}

func TestLoadedServerKeyFingerprintMatchesGotd(t *testing.T) {
	key, err := tlrpccrypto.GenerateServerKey()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	path := t.TempDir() + "/server.pem"
	if err := tlrpccrypto.SavePEMPrivateKey(path, key); err != nil {
		t.Fatalf("save server key: %v", err)
	}
	loaded, err := tlrpccrypto.LoadPEMPrivateKey(path)
	if err != nil {
		t.Fatalf("load server key: %v", err)
	}
	want := (telegram.PublicKey{RSA: &loaded.Key.PublicKey}).Fingerprint()
	if loaded.ID != want {
		t.Fatalf("loaded fingerprint = %d, gotd = %d", loaded.ID, want)
	}
}

func TestLoadPEMPrivateKeyAcceptsPKCS1AndPKCS8(t *testing.T) {
	key, err := tlrpccrypto.GenerateServerKey()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	pkcs8, err := x509.MarshalPKCS8PrivateKey(key.Key)
	if err != nil {
		t.Fatalf("marshal PKCS#8 key: %v", err)
	}

	tests := []struct {
		name      string
		blockType string
		der       []byte
	}{
		{name: "PKCS#1", blockType: "RSA PRIVATE KEY", der: x509.MarshalPKCS1PrivateKey(key.Key)},
		{name: "PKCS#8", blockType: "PRIVATE KEY", der: pkcs8},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := t.TempDir() + "/server.pem"
			contents := pem.EncodeToMemory(&pem.Block{Type: test.blockType, Bytes: test.der})
			if err := os.WriteFile(path, contents, 0600); err != nil {
				t.Fatalf("write key: %v", err)
			}
			loaded, err := tlrpccrypto.LoadPEMPrivateKey(path)
			if err != nil {
				t.Fatalf("load key: %v", err)
			}
			if loaded.ID != key.ID {
				t.Fatalf("fingerprint = %d, want %d", loaded.ID, key.ID)
			}
		})
	}
}
