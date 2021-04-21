#!/bin/sh

echo "Generating faraday gRPC server protos"

# Generate the protos.
protoc -I/usr/local/include -I. \
       -I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
       --go_out=plugins=grpc,paths=source_relative:. \
       faraday.proto

# Generate the REST reverse proxy.
protoc -I/usr/local/include -I. \
  -I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
  --grpc-gateway_out=logtostderr=true,paths=source_relative:. \
  faraday.proto

# Finally, generate the swagger file which describes the REST API in detail.
protoc -I/usr/local/include -I. \
  -I$GOPATH/src/github.com/grpc-ecosystem/grpc-gateway/third_party/googleapis \
  --swagger_out=logtostderr=true:. \
  faraday.proto
