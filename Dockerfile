FROM golang
LABEL maintainer="Jem Gunay"

# set up go components
ADD . /go/src/github.com/jemgunay/memoryshare
CMD ["/go/src/github.com/jemgunay/memoryshare/memoryshare", "-log_verbosity=3"]

EXPOSE 8000