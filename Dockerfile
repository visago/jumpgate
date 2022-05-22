FROM golang:alpine AS build
RUN apk add git make

RUN mkdir /build
ADD . /build/
WORKDIR /build
RUN make

FROM alpine
COPY --from=build /build/bin/jumpgate /jumpgate/jumpgate
RUN adduser -S -D -H -h /jumpgate jumpgate && chown jumpgate: /jumpgate/jumpgate && chmod +x /jumpgate/jumpgate
USER jumpgate
ENTRYPOINT ["/jumpgate/jumpgate"]
