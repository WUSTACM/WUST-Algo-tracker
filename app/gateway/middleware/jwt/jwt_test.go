package jwt

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	config "github.com/go-kratos/gateway/api/gateway/config/v1"
	v1 "github.com/go-kratos/gateway/api/gateway/middleware/jwt/v1"
	"github.com/go-kratos/gateway/middleware"
	jwtlib "github.com/golang-jwt/jwt/v5"
	"google.golang.org/protobuf/types/known/anypb"
)

func testJWTSecret() string {
	return strings.Repeat("a", 32)
}

func jwtConfig(paths ...string) *config.Middleware {
	options, err := anypb.New(&v1.Jwt{
		Secret:      testJWTSecret(),
		PublicPaths: paths,
	})
	if err != nil {
		panic(err)
	}
	return &config.Middleware{Options: options}
}

func roundTripper(status int) http.RoundTripper {
	return middleware.RoundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: status, Body: io.NopCloser(http.NoBody)}, nil
	})
}

func signedToken(t *testing.T) string {
	t.Helper()
	token, err := jwtlib.NewWithClaims(jwtlib.SigningMethodHS256, jwtlib.MapClaims{
		"sub": "test-user",
		"exp": time.Now().Add(time.Hour).Unix(),
	}).SignedString([]byte(testJWTSecret()))
	if err != nil {
		t.Fatalf("SignedString returned error: %v", err)
	}
	return token
}

func TestMiddlewareRejectsMissingSecret(t *testing.T) {
	if _, err := Middleware(&config.Middleware{}); err == nil {
		t.Fatal("Middleware accepted a missing JWT secret")
	}
}

func TestMiddlewareAllowsPublicPathWithoutToken(t *testing.T) {
	m, err := Middleware(jwtConfig("/public"))
	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com/public?x=1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := m(roundTripper(http.StatusNoContent)).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestMiddlewareRejectsMissingTokenOnPrivatePath(t *testing.T) {
	m, err := Middleware(jwtConfig("/public"))
	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com/private", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := m(roundTripper(http.StatusNoContent)).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusUnauthorized)
	}
}

func TestMiddlewareAcceptsSignedToken(t *testing.T) {
	m, err := Middleware(jwtConfig("/public"))
	if err != nil {
		t.Fatalf("Middleware returned error: %v", err)
	}
	req, err := http.NewRequest(http.MethodGet, "http://example.com/private", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+signedToken(t))
	resp, err := m(roundTripper(http.StatusNoContent)).RoundTrip(req)
	if err != nil {
		t.Fatalf("RoundTrip returned error: %v", err)
	}
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}
