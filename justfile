# Run `just` to list the recipes.

[private]
default:
    @just --list --unsorted

# Run the service at http://localhost:8080 (pass staging or production to preview those tags)
run env="local":
    APP_ENV={{env}} go run .

# Vet and test
test:
    go vet ./...
    go test ./...

# Build the image locally, tagged back-hello:local
image:
    docker build --build-arg VERSION=local -t back-hello:local .
