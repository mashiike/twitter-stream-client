GIT_VER := $(shell git describe --tags)
DATE := $(shell date +%Y-%m-%dT%H:%M:%S%z)
export GO111MODULE := on

.PHONY: test binary install clean

cmd/twitter-stream-client/twitter-stream-client: *.go cmd/twitter-stream-client/*.go go.* appspec/*.go
	cd cmd/twitter-stream-client && go build -ldflags "-s -w -X main.Version=${GIT_VER} -X main.buildDate=${DATE}" -gcflags="-trimpath=${PWD}"

install: cmd/twitter-stream-client/twitter-stream-client
	install cmd/twitter-stream-client/twitter-stream-client ${GOPATH}/bin

test:
	go test -race ./...

packages:
	cd cmd/twitter-stream-client && gox -os="linux darwin" -arch="amd64" -output "../../pkg/{{.Dir}}-${GIT_VER}-{{.OS}}-{{.Arch}}" -ldflags "-w -s -X main.Version=${GIT_VER} -X main.buildDate=${DATE}"
	cd pkg && find . -name "*${GIT_VER}*" -type f -exec zip {}.zip {} \;

clean:
	rm -f cmd/twitter-stream-client/twitter-stream-client
	rm -f pkg/*

release:
	ghr -prerelease -u mashiike -r twitter-stream-client -n "$(GIT_VER)" $(GIT_VER) pkg/

ci-test:
	$(MAKE) install
	cd tests/ci && PATH=${GOPATH}/bin:$PATH $(MAKE) test
