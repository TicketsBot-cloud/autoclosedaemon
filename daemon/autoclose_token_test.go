package daemon

import (
	"errors"
	"strings"
	"testing"

	"github.com/TicketsBot-cloud/common/encryption"
	"github.com/TicketsBot-cloud/database"
)

const (
	testKeyA = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="
	testKeyB = "ICEiIyQlJicoKSorLC0uLzAxMjM0NTY3ODk6Ozw9Pj8="
)

func TestWhitelabelToken(t *testing.T) {
	const (
		botId  = uint64(100)
		secret = "sealed-secret-token"
	)

	ring, err := encryption.ParseKeyring("1:"+testKeyA, "1")
	if err != nil {
		t.Fatal(err)
	}

	otherRing, err := encryption.ParseKeyring("1:"+testKeyB, "1")
	if err != nil {
		t.Fatal(err)
	}

	sealed, version, err := ring.Seal(botId, secret)
	if err != nil {
		t.Fatal(err)
	}

	legacy, empty := "legacy-token", ""
	unknownVersion := int16(9)

	tests := []struct {
		name    string
		ring    *encryption.Keyring
		bot     database.WhitelabelBot
		want    string
		wantErr bool
	}{
		{name: "no row has no credential", ring: ring, bot: database.WhitelabelBot{}},
		{name: "sealed token", ring: ring, bot: database.WhitelabelBot{BotId: botId, TokenCiphertext: sealed, TokenKeyVersion: &version}, want: secret},
		{name: "empty plaintext uses sealed token", ring: ring, bot: database.WhitelabelBot{BotId: botId, Token: &empty, TokenCiphertext: sealed, TokenKeyVersion: &version}, want: secret},
		{name: "plaintext wins", ring: ring, bot: database.WhitelabelBot{BotId: botId, Token: &legacy, TokenCiphertext: sealed, TokenKeyVersion: &version}, want: legacy},
		{name: "wrong key", ring: otherRing, bot: database.WhitelabelBot{BotId: botId, TokenCiphertext: sealed, TokenKeyVersion: &version}, wantErr: true},
		{name: "ciphertext from another bot", ring: ring, bot: database.WhitelabelBot{BotId: botId + 1, TokenCiphertext: sealed, TokenKeyVersion: &version}, wantErr: true},
		{name: "unknown key version", ring: ring, bot: database.WhitelabelBot{BotId: botId, TokenCiphertext: sealed, TokenKeyVersion: &unknownVersion}, wantErr: true},
		{name: "row without a token", ring: ring, bot: database.WhitelabelBot{BotId: botId}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := whitelabelToken(tt.ring, tt.bot)
			if tt.wantErr {
				if !errors.Is(err, errTokenUnavailable) {
					t.Fatalf("error = %v, want errTokenUnavailable", err)
				}

				if got != "" || strings.Contains(err.Error(), secret) {
					t.Fatal("failure exposed a token")
				}

				return
			}

			if err != nil {
				t.Fatal(err)
			}

			if got != tt.want {
				t.Fatal("unexpected token")
			}
		})
	}
}
