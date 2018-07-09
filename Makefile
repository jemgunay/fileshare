# Go parameters
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test
GOVET=$(GOCMD) vet
BINARY_NAME=memoryshare
BINARY_WIN=$(BINARY_NAME).exe

all: clean test build

clean:
	rm -rf ./build
	mkdir -p ./build/cmd/{linux,windows}

test:
	$(GOTEST) -v ./...
	$(GOVET)

build: dep build-linux build-windows
	cp ./config/settings.ini ./build/
	cp -r ./dynamic ./build/
	mkdir -p ./build/static && cp -r ./static/{css,fonts,img,js,templates} ./build/static/
	mkdir -p ./build/config && cp ./config/settings.ini ./build/config/
	cd ./build && zip -r ./memoryshare.zip *

dep:
	go get ./...

build-linux:
	cd ./cmd/memoryshare; \
	$(GOBUILD) -o $(BINARY_NAME); \
	mv $(BINARY_NAME) ../../build/cmd/linux/

build-windows:
	cd ./cmd/memoryshare; \
	GOOS=windows GOARCH=386 $(GOBUILD) -o $(BINARY_WIN); \
	mv $(BINARY_WIN) ../../build/cmd/windows/