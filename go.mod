module github.com/TicketsBot/autoclosedaemon

go 1.22.0

toolchain go1.22.4

// replace github.com/TicketsBot-cloud/database => ../database

// replace github.com/TicketsBot-cloud/common => ../common

// replace github.com/TicketsBot-cloud/gdl => ../gdl

require (
	github.com/TicketsBot-cloud/common v0.0.0-20260827064609-69131fc7bd3e
	github.com/TicketsBot-cloud/gdl v0.0.0-20260612070331-a3947b410d3e
	github.com/caarlos0/env v3.5.0+incompatible
	github.com/getsentry/sentry-go v0.21.0
	github.com/go-redis/redis/v8 v8.11.3
	github.com/jackc/pgx/v4 v4.18.3
	go.uber.org/zap v1.27.1
)

require golang.org/x/exp v0.0.0-20241108190413-2d47ceb2692f // indirect

require (
	github.com/TicketsBot-cloud/database v0.0.0-20260827185255-75ab724b0bca
	github.com/TicketsBot/ttlcache v1.6.1-0.20200405150101-acc18e37b261 // indirect
	github.com/boltdb/bolt v1.3.1 // indirect
	github.com/cespare/xxhash/v2 v2.1.1 // indirect
	github.com/dgryski/go-rendezvous v0.0.0-20200823014737-9f7001d12a5f // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/jackc/chunkreader/v2 v2.0.1 // indirect
	github.com/jackc/pgconn v1.14.3 // indirect
	github.com/jackc/pgio v1.0.0 // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgproto3/v2 v2.3.3 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgtype v1.14.0 // indirect
	github.com/jackc/pgx v3.6.2+incompatible // indirect
	github.com/jackc/puddle v1.3.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/juju/ratelimit v1.0.1 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/pasztorpisti/qs v0.0.0-20171216220353-8d6c33ee906c // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/sirupsen/logrus v1.9.0 // indirect
	go.uber.org/multierr v1.10.0 // indirect
	golang.org/x/crypto v0.28.0 // indirect
	golang.org/x/sync v0.9.0 // indirect
	golang.org/x/sys v0.27.0 // indirect
	golang.org/x/text v0.19.0 // indirect
)
