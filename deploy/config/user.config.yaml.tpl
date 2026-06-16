server:
  http:
    addr: ${USER_HTTP_ADDR}
    timeout: 5s
  grpc:
    addr: ${USER_GRPC_ADDR}
    timeout: 5s
  reg_dsn: ${CONSUL_HOST}:${CONSUL_PORT}
  jwt_secret: ${JWT_SECRET}
data:
  database:
    driver: postgres
    source: host=${POSTGRES_HOST} user=${POSTGRES_USER} password=${POSTGRES_PASSWORD} dbname=${POSTGRES_USER_DB} port=${POSTGRES_PORT} sslmode=disable TimeZone=Asia/Shanghai
  redis:
    addr: ${REDIS_HOST}:${REDIS_PORT}
    password: ${REDIS_PASSWORD}
    read_timeout: 1s
    write_timeout: 1s
