#!/bin/sh

echo "Generating faraday gRPC server protos"

# Generate the protos.
protoc --go_out=plugins=grpc,paths=source_relative:. \
  faraday.proto

# Generate the REST reverse proxy.
protoc \
  --grpc-gateway_out=logtostderr=true,paths=source_relative,grpc_api_configuration=rest-annotations.yaml:. \
  faraday.proto

# Finally, generate the swagger file which describes the REST API in detail.
protoc \
  --swagger_out=logtostderr=true,grpc_api_configuration=rest-annotations.yaml:. \
  faraday.proto
