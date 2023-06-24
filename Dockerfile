# We're going to use the official Golang image as the base image
FROM golang:1.20-alpine as builder

# Set the current working directory inside the container
WORKDIR /app

# Copy the go mod and sum files, then download modules
COPY go.mod go.sum ./
RUN go mod download

# Copy the entire project 
COPY . .

# Build the Go app
RUN CGO_ENABLED=0 GOOS=linux go build -o main .

# Use a new stage to create a clean final image
FROM alpine:latest

WORKDIR /root/

# Copy the pre-built binary file from the previous stage
COPY --from=builder /app/main .

# Command to run the executable
CMD ["./main"]
