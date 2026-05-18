# Multi-stage Docker build for the Pulse Chat server.
#
# Stage 1 (`builder`) compiles the Go binary against a full Go toolchain.
# Stage 2 copies just the binary into a tiny distroless image. The final
# image is small (~25 MB) and contains nothing but the binary + CA certs.
#
# Why multi-stage? Because if you `COPY` your whole repo into the final
# image, you ship your Go toolchain, build cache, source code, and tests
# to production. That's bloat, attack surface, and a giveaway of internals.
#
# Why distroless? It contains no shell, no package manager, no extra
# binaries. Smaller attack surface than alpine; smaller still than debian-
# slim. The trade-off: you can't `docker exec -it <container> sh` to poke
# around. That's a feature, not a bug, in production.

# -----------------------------------------------------------------------------
# Stage 1 — build
# -----------------------------------------------------------------------------
FROM golang:1.23-alpine AS builder

WORKDIR /src

# Download deps first (separate layer) so subsequent builds with unchanged
# go.mod/go.sum hit the build cache.
COPY go.mod go.sum* ./
RUN go mod download

# Now copy the rest of the source.
COPY . .

# Build the server.
# - CGO_DISABLED so we produce a static binary (no libc dependency).
# - -trimpath removes absolute paths from the binary (reproducible builds).
# - -ldflags "-s -w" strips the symbol table and DWARF for smaller binary.
#
# The output binary lives at /out/server.
RUN CGO_ENABLED=0 GOOS=linux \
    go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/server \
    ./cmd/server

# -----------------------------------------------------------------------------
# Stage 2 — runtime
# -----------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

# Use the unprivileged user provided by the distroless image. Containers
# should never run as root unless they have a specific reason.
USER nonroot:nonroot

# Copy ONLY the binary. Nothing else from the build stage.
COPY --from=builder /out/server /server

# Expose the default port. This is documentation — it does not actually
# publish anything; the `docker run -p` flag (or compose) does that.
EXPOSE 8080

# `exec` form so the binary becomes PID 1 (receives signals directly).
ENTRYPOINT ["/server"]
