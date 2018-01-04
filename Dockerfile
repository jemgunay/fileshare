FROM golang
LABEL maintainer="Jem Gunay"

# set up go components
ADD . /go/src/github.com/jemgunay/memoryshare
RUN mv /go/src/github.com/jemgunay/memoryshare/memoryshare /go/bin/
CMD ["/go/bin/memoryshare", "-log_verbosity=3"]

EXPOSE 8000