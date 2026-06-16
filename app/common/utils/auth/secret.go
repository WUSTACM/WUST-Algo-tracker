package auth

import (
	"cwxu-algo/app/common/conf"
	"fmt"
	"strings"
)

func JWTSecretBytes(c *conf.Server) ([]byte, error) {
	if c == nil {
		return nil, fmt.Errorf("server config is nil")
	}
	secret := strings.TrimSpace(c.GetJwtSecret())
	if secret == "" {
		return nil, fmt.Errorf("server.jwt_secret is required")
	}
	if len(secret) < 32 {
		return nil, fmt.Errorf("server.jwt_secret must be at least 32 characters")
	}
	return []byte(secret), nil
}
