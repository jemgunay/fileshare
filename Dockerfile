FROM golang
LABEL maintainer="Jem Gunay"

ADD . /go/src/github.com/jemgunay/fileshare
RUN go get github.com/jemgunay/fileshare/...
RUN go install github.com/jemgunay/fileshare
ENTRYPOINT /go/bin/fileshare

EXPOSE 8000