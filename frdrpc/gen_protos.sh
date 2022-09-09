#!/bin/bash

set -e

# generate compiles the *.pb.go stubs from the *.proto files.
function generate() {
  echo "Generating faraday gRPC server protos"

  # Generate the protos.
  protoc -I/usr/local/include -I. \
    --go_out . --go_opt paths=source_relative \
    --go-grpc_out . --go-grpc_opt paths=source_relative \
    faraday.proto

  # Generate the REST reverse proxy.
  protoc -I/usr/local/include -I. \
    --grpc-gateway_out . \
    --grpc-gateway_opt logtostderr=true \
    --grpc-gateway_opt paths=source_relative \
    --grpc-gateway_opt grpc_api_configuration=faraday.yaml \
    faraday.proto

  # Finally, generate the swagger file which describes the REST API in detail.
  protoc -I/usr/local/include -I. \
    --openapiv2_out . \
    --openapiv2_opt logtostderr=true \
    --openapiv2_opt grpc_api_configuration=faraday.yaml \
    --openapiv2_opt json_names_for_fields=false \
    faraday.proto
  
  # Generate the JSON/WASM client stubs.
  falafel=$(which falafel)
  pkg="frdrpc"
  opts="package_name=$pkg,api_prefix=1,js_stubs=1"
  protoc -I/usr/local/include -I. -I.. \
    --plugin=protoc-gen-custom=$falafel\
    --custom_out=. \
    --custom_opt="$opts" \
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
