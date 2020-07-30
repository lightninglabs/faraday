# Faraday integration test dockerfile

FROM golang:1.13-alpine

ARG BITCOIND_VERSION=0.19.1
ARG GLIBC_VERSION=2.29-r0
ARG LND_VERSION=v0.11.0-beta.rc2

WORKDIR /root

RUN apk add --no-cache git gcc musl-dev make curl bash jq

# Install glibc (for bitcoind)
RUN wget -q -O /etc/apk/keys/sgerrand.rsa.pub https://alpine-pkgs.sgerrand.com/sgerrand.rsa.pub \
  && wget https://github.com/sgerrand/alpine-pkg-glibc/releases/download/${GLIBC_VERSION}/glibc-${GLIBC_VERSION}.apk \
  && wget https://github.com/sgerrand/alpine-pkg-glibc/releases/download/${GLIBC_VERSION}/glibc-bin-${GLIBC_VERSION}.apk \
  && apk --no-cache add glibc-${GLIBC_VERSION}.apk \
  && apk --no-cache add glibc-bin-${GLIBC_VERSION}.apk

# Install bitcoind
RUN wget https://bitcoin.org/bin/bitcoin-core-${BITCOIND_VERSION}/bitcoin-${BITCOIND_VERSION}-x86_64-linux-gnu.tar.gz && \
  tar xvfz bitcoin-${BITCOIND_VERSION}-x86_64-linux-gnu.tar.gz

RUN mkdir .bitcoin \
  && mv bitcoin-${BITCOIND_VERSION}/bin/* /usr/local/bin/

# Get lnd sources and install. Can't use go get here, because tags aren't passed
# as a compiler variable, leading to lnd not reporting the tags that it is built
# with.
RUN git clone https://github.com/lightningnetwork/lnd.git && \
  cd lnd && git checkout ${LND_VERSION} && \
  make install tags="signrpc walletrpc chainrpc invoicesrpc"

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
