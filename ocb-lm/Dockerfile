# build custom otelcol binary
FROM golang:1.24-alpine AS otel-collector-builder
USER root
RUN apk --update add ca-certificates git tzdata
WORKDIR /build
ARG VERSION=v0.135.0
RUN go install go.opentelemetry.io/collector/cmd/builder@${VERSION}
COPY ocb-config.yaml .
RUN builder --config=ocb-config.yaml

# minimal image with standard CA certs, zoneinfo and custom otelcol binary
FROM scratch
ARG USER_UID=10001
ENV LOG_LEVEL="WARN"
ENV ENVIRONMENT="local"
ENV APP_MONIKER="local"
ENV APP_NAME="local"
ENV APP_REGION="local"
USER ${USER_UID}
COPY --from=otel-collector-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=otel-collector-builder /usr/share/zoneinfo /usr/share/zoneinfo
COPY --from=otel-collector-builder /tmp/otelcol/otelcol /otelcol
COPY ./opentelemetry-collector/otel-config.yaml /etc/config.yaml
ENTRYPOINT ["/otelcol"]
CMD ["--config", "/etc/config.yaml"]