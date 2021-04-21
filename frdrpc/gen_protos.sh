#!/bin/bash

set -e

# generate compiles the *.pb.go stubs from the *.proto files.
function generate() {
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
}

# format formats the *.proto files with the clang-format utility.
function format() {
  find . -name "*.proto" -print0 | xargs -0 clang-format --style=file -i
}

# Compile and format the frdrpc package.
pushd frdrpc
format
generate
popd
