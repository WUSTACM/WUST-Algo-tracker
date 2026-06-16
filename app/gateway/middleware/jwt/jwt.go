package jwt

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strings"

	config "github.com/go-kratos/gateway/api/gateway/config/v1"
	v1 "github.com/go-kratos/gateway/api/gateway/middleware/jwt/v1"
	"github.com/go-kratos/gateway/middleware"
	"github.com/golang-jwt/jwt/v5"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

var defaultPublicPaths = []string{
	"/v1/user/auth/login",
	"/v1/user/auth/register",
	"/v1/user/profile/get-by-id",
	"/v1/user/profile/get-by-name",
	"/v1/user/profile/list",
	"/v1/user/role/list",
	"/v1/user/group/get",
	"/v1/user/group/list",
	"/v1/user/team/detail",
	"/v1/core/submit-log/get-by-id",
	"/v1/core/contest/list",
	"/v1/core/contest/ranking",
	"/v1/core/statistic/heatmap",
	"/v1/core/statistic/period",
	"/v1/core/statistic/platform-period",
	"/v1/core/statistic/team-period",
	"/v1/core/statistic/explanation",
	"/v1/core/statistic/platform-detail",
	"/v1/core/achievement/global-snapshot",
	"/v1/core/bulletin/get",
	"/v1/core/bulletin/list",
}

func init() {
	middleware.Register("jwt", Middleware)
}

// Middleware jwt 校验中间件
func Middleware(c *config.Middleware) (middleware.Middleware, error) {
	options, err := jwtOptions(c)
	if err != nil {
		return nil, err
	}
	secret := []byte(options.Secret)
	publicPaths := options.PublicPaths
	if len(publicPaths) == 0 {
		publicPaths = defaultPublicPaths
	}
	return func(next http.RoundTripper) http.RoundTripper {
		return middleware.RoundTripperFunc(func(request *http.Request) (*http.Response, error) {

			// 公开接口放行
			if isPublicPath(request.URL.Path, publicPaths) {
				return next.RoundTrip(request)
			}
			authHeader := request.Header.Get("Authorization")
			tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
			if tokenStr == authHeader {
				return buildUnauthorizedResp("JWT Token not found"), nil
			}
			token, err := jwt.Parse(tokenStr, func(token *jwt.Token) (interface{}, error) {
				if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
					return nil, fmt.Errorf("unexpected jwt signing method: %s", token.Method.Alg())
				}
				return secret, nil
			})
			if err != nil || !token.Valid {
				return buildUnauthorizedResp("JWT Token invalid"), nil
			}
			return next.RoundTrip(request)
		})
	}, nil

}

func isPublicPath(path string, publicPaths []string) bool {
	for _, p := range publicPaths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if strings.HasSuffix(p, "*") {
			if strings.HasPrefix(path, strings.TrimSuffix(p, "*")) {
				return true
			}
			continue
		}
		if path == p {
			return true
		}
	}
	return false
}

func jwtOptions(c *config.Middleware) (*v1.Jwt, error) {
	options := &v1.Jwt{}
	if c != nil && c.Options != nil {
		if err := anypb.UnmarshalTo(c.Options, options, proto.UnmarshalOptions{Merge: true}); err != nil {
			return nil, err
		}
	}
	options.Secret = strings.TrimSpace(options.Secret)
	if options.Secret == "" {
		return nil, fmt.Errorf("jwt middleware secret is required")
	}
	if len(options.Secret) < 32 {
		return nil, fmt.Errorf("jwt middleware secret must be at least 32 characters")
	}
	return options, nil
}

func buildUnauthorizedResp(msg string) *http.Response {
	return &http.Response{StatusCode: http.StatusUnauthorized, Body: io.NopCloser(bytes.NewBufferString(msg))}
}
