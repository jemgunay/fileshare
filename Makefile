# build for linux/windows/rpi & zip all required service files
GOCMD=go
GOBUILD=$(GOCMD) build
GOTEST=$(GOCMD) test -race
GOVET=$(GOCMD) vet
BINARY_NAME=memoryshare
BINARY_NAME_WIN=$(BINARY_NAME).exe
# extract current service version from settings.ini config file
SERVICE_VERSION=$$(awk -F '=' '/^version/ {print $$2}' config/settings.ini | tr -d ' ' | tr -d '"')

all: clean test build

clean:
	rm -rf ./build/{cmd,config,dynamic,static}

test:
	$(GOTEST)
	$(GOVET)

# build for all environments
build: dep build-linux build-windows build-rpi
	rm -f ./memoryshare_$(SERVICE_VERSION).zip
	cp -r ./dynamic ./build/
	mkdir -p ./build/static && cp -r ./static/css ./static/fonts ./static/img ./static/js ./static/templates ./build/static/
	mkdir -p ./build/config && cp ./config/settings_default.ini ./build/config/settings.ini
	cd ./build && rm -f ./memoryshare_$(SERVICE_VERSION).zip && zip -x "*.zip" -r ./memoryshare_$(SERVICE_VERSION).zip *

# pull down all dependencies
dep:
	go get ./...

# build linux executable
build-linux:
	mkdir -p ./build/cmd/linux; \
	cd ./cmd/memoryshare; \
	$(GOBUILD) -o ../../build/cmd/linux/$(BINARY_NAME)

# build windows executable
build-windows:
	mkdir -p ./build/cmd/windows; \
	cd ./cmd/memoryshare; \
	GOOS=windows GOARCH=386 $(GOBUILD) -o ../../build/cmd/windows/$(BINARY_NAME_WIN)

# build ARM executable for raspberry pi
build-rpi:
	mkdir -p ./build/cmd/rpi; \
	cd ./cmd/memoryshare; \
	GOOS=linux GOARCH=arm GOARM=5 $(GOBUILD) -o ../../build/cmd/rpi/$(BINARY_NAME)

