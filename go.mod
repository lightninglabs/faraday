module github.com/lightninglabs/faraday

require (
	github.com/btcsuite/btcd v0.20.1-beta.0.20200515232429-9f0179fd2c46
	github.com/btcsuite/btclog v0.0.0-20170628155309-84c8d2346e9f
	github.com/golang/protobuf v1.3.3
	github.com/grpc-ecosystem/grpc-gateway v1.14.3
	github.com/jessevdk/go-flags v1.4.0
	github.com/lightninglabs/loop v0.6.0-beta
	github.com/lightninglabs/protobuf-hex-display v1.3.3-0.20191212020323-b444784ce75d
	github.com/lightningnetwork/lnd v0.10.0-beta.rc6.0.20200603030653-09bb9db78246
	github.com/shopspring/decimal v1.2.0
	github.com/stretchr/testify v1.4.0
	github.com/urfave/cli v1.20.0
	google.golang.org/genproto v0.0.0-20190927181202-20e1ac93f88c
	google.golang.org/grpc v1.27.0
	gopkg.in/macaroon.v2 v2.1.0
)

go 1.13

replace github.com/lightninglabs/loop => github.com/joostjager/loop v0.2.4-alpha.0.20200616093711-db69720acc7e
