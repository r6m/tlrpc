package crypto_test

import (
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
