get_vendor_deps:
	go get -u -v github.com/golang/dep/cmd/dep
	dep ensure -v

install:
	go install ./
	go install ./alerter