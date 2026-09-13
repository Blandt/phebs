package t421

import (
	"encoding/base64"
	"encoding/binary"
	"strings"
	"testing"
)

func TestDeriveExecutionSignerPublicRejectsNoncanonicalKeys(t *testing.T) {
	blob := make([]byte, 4+len("ssh-ed25519")+4+32)
	binary.BigEndian.PutUint32(blob, uint32(len("ssh-ed25519")))
	copy(blob[4:], "ssh-ed25519")
	offset := 4 + len("ssh-ed25519")
	binary.BigEndian.PutUint32(blob[offset:], 32)
	for index := offset + 4; index < len(blob); index++ {
		blob[index] = byte(index)
	}
	good := []byte("ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob) + "\n")
	canonical, fingerprint, publicSHA256, err := deriveExecutionSignerPublic(good)
	if err != nil || string(canonical) != string(good) || !validSSHSHA256Fingerprint(fingerprint) || !validExecutionHexSHA256(publicSHA256) {
		t.Fatal("valid RFC4253 Ed25519 public key was refused", err)
	}
	for _, test := range []struct {
		name string
		raw  []byte
	}{
		{"missing LF", good[:len(good)-1]},
		{"extra LF", append(append([]byte(nil), good...), '\n')},
		{"comment", []byte(strings.TrimSuffix(string(good), "\n") + " comment\n")},
		{"wrong algorithm", []byte("ssh-rsa " + base64.StdEncoding.EncodeToString(blob) + "\n")},
		{"bad base64", []byte("ssh-ed25519 !!!\n")},
		{"short blob", []byte("ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob[:len(blob)-1]) + "\n")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, _, _, err := deriveExecutionSignerPublic(test.raw); err == nil {
				t.Fatal("noncanonical public key was admitted")
			}
		})
	}
}
