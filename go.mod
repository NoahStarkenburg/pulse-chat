module github.com/NoahStarkenburg/pulse-chat

go 1.23

// Phase 1 dependencies — added when you run `go mod tidy` after writing the
// first code that imports them. Listed here for orientation only:
//
//   github.com/coder/websocket    // WebSocket protocol implementation
//   github.com/go-chi/chi/v5      // Lean HTTP router
//
// Later phases will add:
//
//   github.com/jackc/pgx/v5                          // Phase 2: Postgres
//   github.com/redis/go-redis/v9                     // Phases 3+4: Redis
//   github.com/rabbitmq/amqp091-go                   // Phase 5: RabbitMQ
//   google.golang.org/grpc                           // Phase 5b: gRPC runtime
//   google.golang.org/protobuf                       // Phase 5b: protobuf runtime
//   github.com/prometheus/client_golang              // Phase 6: metrics
//   github.com/testcontainers/testcontainers-go      // tests: real infra

require (
	github.com/coder/websocket v1.8.14
	github.com/google/uuid v1.6.0
)
