package auth

import (
	"cwxu-algo/app/common/conf"
	"strings"
	"testing"
)

func TestJWTSecretBytes(t *testing.T) {
	secret := strings.Repeat("a", 32)
	got, err := JWTSecretBytes(&conf.Server{JwtSecret: secret})
	if err != nil {
		t.Fatalf("JWTSecretBytes returned error: %v", err)
	}
	if string(got) != secret {
		t.Fatalf("JWTSecretBytes = %q, want %q", string(got), secret)
	}
}

func TestJWTSecretBytesRejectsMissingOrShortSecret(t *testing.T) {
	tests := []*conf.Server{
		nil,
		{},
		{JwtSecret: "short"},
	}
	for _, tt := range tests {
		if _, err := JWTSecretBytes(tt); err == nil {
			t.Fatalf("JWTSecretBytes(%+v) returned nil error", tt)
		}
	}
}
