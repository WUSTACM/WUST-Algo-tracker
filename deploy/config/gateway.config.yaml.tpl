name: wust-algo
version: v1
middlewares:
  - name: jwt
    required: true
    options:
      '@type': type.googleapis.com/gateway.middleware.jwt.v1.Jwt
      secret: ${JWT_SECRET}
      publicPaths:
        - /v1/user/auth/login
        - /v1/user/auth/register
        - /v1/user/profile/get-by-id
        - /v1/user/profile/get-by-name
        - /v1/user/profile/list
        - /v1/user/role/list
        - /v1/user/group/get
        - /v1/user/group/list
        - /v1/user/team/detail
        - /v1/core/submit-log/get-by-id
        - /v1/core/contest/list
        - /v1/core/contest/ranking
        - /v1/core/statistic/heatmap
        - /v1/core/statistic/period
        - /v1/core/statistic/platform-period
        - /v1/core/statistic/team-period
        - /v1/core/statistic/explanation
        - /v1/core/statistic/platform-detail
        - /v1/core/achievement/global-snapshot
        - /v1/core/bulletin/get
        - /v1/core/bulletin/list
endpoints:
  - path: /v1/user/*
    timeout: 10s
    protocol: HTTP
    backends:
      - target: 'discovery:///user'
  - path: /v1/core/*
    timeout: 20s
    protocol: HTTP
    backends:
      - target: 'discovery:///core-data'
  - path: /v1/agent/*
    timeout: 30s
    protocol: HTTP
    backends:
      - target: 'discovery:///agent'
