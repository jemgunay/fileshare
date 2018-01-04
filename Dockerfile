FROM golang
LABEL maintainer="Jem Gunay"

ADD . /go/src/github.com/jemgunay/fileshare
RUN go get github.com/jemgunay/fileshare/...
RUN go install github.com/jemgunay/fileshare
CMD ["/go/bin/fileshare", "-log_verbosity=3"]

EXPOSE 8000