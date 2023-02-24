# Faraday integration test dockerfile.  Uses two build stages with different
# base images. The first stage builds lnd with the golang base image.
# The second stage runs directly on the bitcoind base image and adds all
# binaries required to run the tests with.
FROM golang:1.19.4-alpine as builder

ARG LND_VERSION=v0.15.4-beta

RUN apk add --no-cache git make

# Get lnd sources and install. Can't use go get here, because tags aren't passed
# as a compiler variable, leading to lnd not reporting the tags that it is built
# with.
RUN git clone https://github.com/lightningnetwork/lnd.git && \
  cd lnd && git checkout ${LND_VERSION} && \
  make install tags="signrpc walletrpc chainrpc invoicesrpc"

# Second build stage, this time we use the bitcoind base image from ruimarinho.
# We use the same image to run bitcoind in our testnet cluster so this should be
# fine for integration tests.
FROM lightninglabs/bitcoin-core:23.0-alpine as final

WORKDIR /root

RUN apk add --no-cache curl bash

# Copy the binaries from the golang builder image.
COPY --from=builder /go/bin/lnd /bin/
COPY --from=builder /go/bin/lncli /bin/

# Copy the integration test executable into the image.
COPY itest.test .
RUN chmod +x itest.test

# Copy the scripts and make them executable.
COPY *.sh ./
RUN chmod +x *.sh

# Copy compiled faraday into the image. Assumption here is that the binary is
# up to date.
COPY faraday .
RUN chmod +x faraday

# Run the test setup.
RUN ./itest_setup.sh

ENTRYPOINT [ "./itest.sh" ]
