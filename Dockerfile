FROM golang:1.10 as build
WORKDIR /go/src/app

# Retrieve dependencies
RUN curl -fsSL -o /usr/local/bin/dep https://github.com/golang/dep/releases/download/v0.4.1/dep-linux-amd64 && chmod +x /usr/local/bin/dep
COPY Gopkg.* ./
RUN dep ensure -vendor-only

# Build the app
COPY . ./
RUN CGO_ENABLED=0 go build -a -o main .

# Final scratch image
FROM scratch
COPY --from=build /go/src/app/main /go/src/app/config.yaml /
ENTRYPOINT ["/main"]
