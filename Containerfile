# Stage 1: Build the Tailwind CSS assets
FROM --platform=$BUILDPLATFORM node:24-alpine AS css-builder

WORKDIR /app

# Copy package config files
COPY package.json yarn.lock .yarnrc.yml ./
COPY tailwind.config.js ./
COPY web/static/css/input.css ./web/static/css/
COPY web/templates/ ./web/templates/

# Compile the static CSS asset
RUN corepack enable && yarn install && yarn build:css

# Stage 2: Build the Go binary
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

WORKDIR /app

# Copy Go dependency files
COPY go.mod go.sum ./
RUN go mod download

# Copy the rest of the source code
COPY . .

# Copy the compiled tailwind.min.css stylesheet from the css-builder stage
COPY --from=css-builder /app/web/static/css/tailwind.min.css ./web/static/css/tailwind.min.css

# Set Go target environment variables automatically supplied by Docker Buildx
ARG TARGETOS
ARG TARGETARCH

# Build statically linked, optimized binary targeting the target platform
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o rgmii_daemon .

# Stage 3: Create a lightweight runtime image
FROM alpine:latest

# Install CA certificates in case HTTPS requests are needed
RUN apk --no-cache add ca-certificates

WORKDIR /app

# Copy the compiled binary from the builder stage
COPY --from=builder /app/rgmii_daemon .

# Expose default HTTP web server port
EXPOSE 8080
ENV QUECTEL_DEBUG="0"

# Run the daemon
ENTRYPOINT ["/app/rgmii_daemon"]
