# Stage 1: Use Go to compile the application into a Linux executable.
FROM golang:1.24.5-alpine AS builder

# Set /app as the working directory for the build process.
WORKDIR /app

# Copy the Go module files to prepare for dependency installation.
COPY go.mod go.sum* ./

# Download the Go dependencies required by the application.
RUN go mod download

# Copy the application source code into the build directory
COPY . .

# Compile the Go application into a smaller Linux executable named server.
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o server .

# Stage 2: Use a lightweight Alpine image to run the compiled application.
FROM alpine:3.19

# Set /app as the working directory for the runtime container.
WORKDIR /app

# Install CA certificates for secure HTTPS connections.
RUN apk --no-cache add ca-certificates

# Copy the compiled server executable from the builder stage.
COPY --from=builder /app/server .

# Document that the application listens on port 5000.
EXPOSE 5000

# Start the compiled Go application when the container runs.
CMD ["./server"]