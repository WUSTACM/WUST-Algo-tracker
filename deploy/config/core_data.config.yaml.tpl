server:
  http:
    addr: ${CORE_HTTP_ADDR}
    timeout: 10s
  grpc:
    addr: ${CORE_GRPC_ADDR}
    timeout: 10s
  reg_dsn: ${CONSUL_HOST}:${CONSUL_PORT}
  amqp_dsn: amqp://${RABBITMQ_USER}:${RABBITMQ_PASSWORD}@${RABBITMQ_HOST}:${RABBITMQ_PORT}/${RABBITMQ_VHOST}
  jwt_secret: ${JWT_SECRET}
data:
  database:
    driver: postgres
    source: host=${POSTGRES_HOST} user=${POSTGRES_USER} password=${POSTGRES_PASSWORD} dbname=${POSTGRES_CORE_DB} port=${POSTGRES_PORT} sslmode=disable TimeZone=Asia/Shanghai
  redis:
    addr: ${REDIS_HOST}:${REDIS_PORT}
    password: ${REDIS_PASSWORD}
    read_timeout: 1s
    write_timeout: 1s
