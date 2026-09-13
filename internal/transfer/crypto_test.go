package transfer

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/eleost04/miyohub/internal/model"
)

const testPassphrase = "synthetic-transfer-passphrase"

func TestArchiveEncryptionRoundTripAndAuthentication(t *testing.T) {
	archive := model.AccountArchive{Version: 1, Accounts: []model.PortableAccount{{Name: "fixture", Cookie: "synthetic-private-marker"}}}
	envelope, err := Seal(archive, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(envelope)
	if bytes.Contains(raw, []byte("fixture")) || bytes.Contains(raw, []byte("synthetic-private-marker")) {
		t.Fatal("plaintext in encrypted envelope")
	}
	got, err := Open(raw, testPassphrase)
	if err != nil || !reflect.DeepEqual(got, archive) {
		t.Fatal("round trip failed", err)
	}
	other, err := Seal(archive, testPassphrase)
	if err != nil || envelope.Salt == other.Salt || envelope.Nonce == other.Nonce || envelope.Ciphertext == other.Ciphertext {
		t.Fatal("encryption not randomized")
	}
	if _, err := Open(raw, "different-test-passphrase"); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	sealed, _ := base64.StdEncoding.DecodeString(envelope.Ciphertext)
	sealed[0] ^= 1
	envelope.Ciphertext = base64.StdEncoding.EncodeToString(sealed)
	raw, _ = json.Marshal(envelope)
	if _, err := Open(raw, testPassphrase); err == nil {
		t.Fatal("tamper accepted")
	}
}

func TestArchiveFormatAndResourceLimits(t *testing.T) {
	e, err := Seal(model.AccountArchive{Version: 1}, testPassphrase)
	if err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Envelope){
		func(e *Envelope) { e.Version = 2 }, func(e *Envelope) { e.Format = "other" },
		func(e *Envelope) { e.KDF = "plain" }, func(e *Envelope) { e.Iterations = 1 },
		func(e *Envelope) { e.Iterations = 2000000000 }, func(e *Envelope) { e.Cipher = "AES-CBC" },
		func(e *Envelope) { e.Nonce = "bad" }, func(e *Envelope) { e.Salt = "bad" },
		func(e *Envelope) { e.Ciphertext = "" },
	} {
		bad := e
		mutate(&bad)
		raw, _ := json.Marshal(bad)
		if _, err := Open(raw, testPassphrase); err == nil {
			t.Fatal("invalid format accepted")
		}
	}
	raw, _ := json.Marshal(e)
	for _, bad := range [][]byte{nil, []byte(`{"cookie":"synthetic"}`), append(raw, []byte(" {}")...), []byte(strings.Repeat(" ", MaxFileBytes+1)), append([]byte(`{"unknown":true,`), raw[1:]...)} {
		if _, err := Open(bad, testPassphrase); err == nil {
			t.Fatal("invalid structure accepted")
		}
	}
	for _, pass := range []string{"", "short", strings.Repeat("x", 129), string([]byte{255})} {
		if err := ValidatePassphrase(pass); err == nil {
			t.Fatal("invalid passphrase accepted")
		}
	}
	if _, err := Seal(model.AccountArchive{Accounts: []model.PortableAccount{{Cookie: strings.Repeat("x", maxPlaintext)}}}, testPassphrase); err == nil {
		t.Fatal("oversized plaintext accepted")
	}
	if err := decodeStrict([]byte(`{"version":1,"accounts":[{"user_id":"forged"}]}`), new(model.AccountArchive)); err == nil {
		t.Fatal("nonportable field accepted")
	}
}
