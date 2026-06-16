name: wust-algo
version: v1
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
