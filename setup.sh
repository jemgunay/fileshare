#!/bin/bash
go get ./...
go build

docker build -t memoryshare_v0.3 . && docker tag memoryshare_v0.3 jemgunay/memoryshare:v0.3 && docker-compose up
