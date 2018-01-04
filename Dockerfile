FROM golang
LABEL maintainer="Jem Gunay"

ADD . /go/src/github.com/jemgunay/memoryshare
RUN go get github.com/jemgunay/memoryshare/...
RUN go install github.com/jemgunay/memoryshare
CMD ["/go/bin/memoryshare", "-log_verbosity=1"]

EXPOSE 8000