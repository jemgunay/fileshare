# Tests, builds for linux/windows & zips all required service files.
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test -race -v ./...
GOVET=$(GOCMD) vet
BINARY_NAME=memoryshare
BINARY_NAME_WIN=$(BINARY_NAME).exe
SERVICE_VERSION=$$(awk -F '=' '/^version/ {print $$2}' config/settings.ini | tr -d ' ' | tr -d '"')

all: clean test build

clean:
	rm -rf ./build/{cmd,config,dynamic,static}

test:
	$(GOTEST)
	$(GOVET)

build: dep build-linux build-windows
	cp -r ./dynamic ./build/
	mkdir -p ./build/static && cp -r ./static/{css,fonts,img,js,templates} ./build/static/
	mkdir -p ./build/config && cp ./config/settings_default.ini ./build/config/settings.ini
	cd ./build && rm -f ./memoryshare_$(SERVICE_VERSION).zip && zip -x "*.zip" -r ./memoryshare_$(SERVICE_VERSION).zip *
	cd .

dep:
	go get ./...

build-linux:
	mkdir -p ./build/cmd/linux; \
	cd ./cmd/memoryshare; \
	$(GOBUILD) -o ../../build/cmd/linux/$(BINARY_NAME)

build-windows:
	mkdir -p ./build/cmd/windows; \
	cd ./cmd/memoryshare; \
	GOOS=windows GOARCH=386 $(GOBUILD) -o ../../build/cmd/windows/$(BINARY_NAME_WIN)
