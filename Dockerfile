ARG porklock_tag=latest
FROM golang:1.12 as build-root

RUN go get -u github.com/jstemmer/go-junit-report

WORKDIR /build

COPY go.mod .
COPY go.sum .

RUN go mod download

COPY . .

ENV CGO_ENABLED=0
ENV GOOS=linux
ENV GOARCH=amd64

RUN go build -ldflags "-X main.appver=$version -X main.gitref=$git_commit" ./...
RUN sh -c "go test -v | tee /dev/stderr | go-junit-report > test-results.xml"



FROM harbor.cyverse.org/de/porklock:${porklock_tag}

COPY --from=build-root /build/vice-file-transfers /
COPY --from=build-root /build/test-results.xml /

ENTRYPOINT ["/vice-file-transfers"]

EXPOSE 60000

ARG git_commit=unknown
ARG version="1.0.0"
ARG descriptive_version=unknown

LABEL org.cyverse.git-ref="$git_commit"
LABEL org.cyverse.version="$version"
LABEL org.cyverse.descriptive-version="$descriptive_version"
LABEL org.label-schema.vcs-ref="$git_commit"
LABEL org.label-schema.vcs-url="https://github.com/cyverse-de/vice-file-transfers"
LABEL org.label-schema.version="$descriptive_version"
