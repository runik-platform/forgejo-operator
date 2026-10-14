FROM docker.io/library/golang:1.26 AS builder

ARG TARGETOS
ARG TARGETARCH

WORKDIR /workspace

COPY go.mod go.sum ./
RUN go mod download

COPY api/ api/
COPY cmd/ cmd/
COPY internal/ internal/

RUN if [ -n "${TARGETARCH}" ]; then export GOARCH="${TARGETARCH}"; fi; \
    CGO_ENABLED=0 GOOS="${TARGETOS:-linux}" go build \
      -trimpath \
      -ldflags="-s -w" \
      -o /manager ./cmd

# Use distroless as minimal base image to package the manager binary
# Refer to https://github.com/GoogleContainerTools/distroless for more details
FROM gcr.io/distroless/static:nonroot

ARG VCS_REF=unknown
ARG VERSION=0.0.18-runik.0
LABEL org.opencontainers.image.title="Forgejo Operator" \
      org.opencontainers.image.source="https://github.com/runik-platform/forgejo-operator" \
      org.opencontainers.image.licenses="Apache-2.0" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${VCS_REF}"

COPY --from=builder /manager /manager
USER 65532:65532

ENTRYPOINT ["/manager"]
