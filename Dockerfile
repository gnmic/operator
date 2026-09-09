# Build the manager binary
# Use BUILDPLATFORM to run Go natively (fast cross-compilation)
FROM --platform=$BUILDPLATFORM golang:1.26.6 AS builder
ARG TARGETOS
ARG TARGETARCH
# Set by the release workflow to the git tag; "dev" for local builds.
ARG VERSION=dev

WORKDIR /workspace
# Copy the Go Modules manifests
COPY go.mod go.mod
COPY go.sum go.sum
# cache deps before building and copying source so that we don't need to re-download as much
# and so that source changes don't invalidate our downloaded layer
RUN go mod download

# Copy the go source
COPY cmd/main.go cmd/main.go
COPY api/ api/
COPY internal/ internal/

# Build
# Go natively cross-compiles - no emulation needed
RUN CGO_ENABLED=0 GOOS=${TARGETOS:-linux} GOARCH=${TARGETARCH} go build -a -ldflags "-s -w -X main.version=${VERSION}" -o manager cmd/main.go

# Use distroless as minimal base image to package the manager binary
# This stage is multi-arch (just copies the pre-built binary)
FROM gcr.io/distroless/static:nonroot
WORKDIR /
COPY --from=builder /workspace/manager .
USER 65532:65532

ENTRYPOINT ["/manager"]
