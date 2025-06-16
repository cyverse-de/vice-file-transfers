ARG porklock_tag=latest

# Build the Go binary
FROM golang:1.24 AS builder
ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64
COPY . /go/src/github.com/cyverse-de/vice-file-transfers
WORKDIR /go/src/github.com/cyverse-de/vice-file-transfers
RUN go build --buildvcs=false .

# Add the Go binary to the porlock image
FROM harbor.cyverse.org/de/porklock:${porklock_tag}
COPY --from=builder /go/src/github.com/cyverse-de/vice-file-transfers/vice-file-transfers /bin/vice-file-transfers
COPY init_working_dir.sh /bin/init_working_dir.sh
ENTRYPOINT ["vice-file-transfers"]

EXPOSE 60000
