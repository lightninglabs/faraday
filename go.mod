module github.com/lightninglabs/faraday

require (
	github.com/btcsuite/btcd v0.20.1-beta.0.20200730232343-1db1b6f8217f
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/btcsuite/btcutil v1.0.2
	github.com/golang/protobuf v1.3.3
	github.com/google/go-cmp v0.3.1 // indirect
	github.com/grpc-ecosystem/grpc-gateway v1.14.3
	github.com/jessevdk/go-flags v1.4.0
	github.com/lightninglabs/lndclient v0.11.0-0
	github.com/lightninglabs/protobuf-hex-display v1.3.3-0.20191212020323-b444784ce75d
	github.com/lightningnetwork/lnd v0.11.1-beta.rc3
	github.com/lightningnetwork/lnd/cert v1.0.3
	github.com/shopspring/decimal v1.2.0
	github.com/stretchr/testify v1.5.1
	github.com/urfave/cli v1.20.0
	golang.org/x/text v0.3.2 // indirect
	google.golang.org/genproto v0.0.0-20190927181202-20e1ac93f88c
	google.golang.org/grpc v1.25.1
	gopkg.in/macaroon-bakery.v2 v2.0.1
	gopkg.in/macaroon.v2 v2.1.0
)

go 1.13

replace github.com/lightninglabs/lndclient => github.com/carlakc/lndclient v0.0.0-20201029074241-0dc151828379
