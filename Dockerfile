# Build a static binary, then copy it into a minimal image: no shell, no package
# manager, running as a non-root user.
FROM golang:1.27.1-alpine@sha256:cf6fca6641884b8433441b2b0652976f975e1d0fdd26d177eaaf8596087f3125 AS build
WORKDIR /src
COPY go.mod ./
COPY *.go ./
COPY web ./web
# The image tag, shown on the page (CI passes sha-<commit>).
ARG VERSION=local
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/hello .

FROM gcr.io/distroless/static-debian13:nonroot@sha256:1c2c046bc09ed40fad370b599a0b1ae7987f55b01e247cf27a7c27cd97e5bbc7
COPY --from=build /out/hello /hello
EXPOSE 8080
ENTRYPOINT ["/hello"]
