FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/fluxport ./cmd/fluxport

FROM alpine:3.23
RUN addgroup -g 10001 fluxport && adduser -D -u 10001 -G fluxport fluxport && mkdir -p /data /state && chown fluxport:fluxport /data /state
COPY --from=build /out/fluxport /usr/local/bin/fluxport
USER fluxport
ENV FLUXPORT_ADDRESS=0.0.0.0:8080 FLUXPORT_DATA_DIR=/data FLUXPORT_STATE_DIR=/state
VOLUME ["/data", "/state"]
EXPOSE 8080
ENTRYPOINT ["fluxport"]
CMD ["serve"]
