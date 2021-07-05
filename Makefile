GO := go
GO_MODULE = github.com/reno-xjb/go-render-redact
PROTO_DIR = ./render/protocol/redact
SRC = ./render

PROTOC = protoc

clean:
	@$(GO) clean -testcache
	rm -rf render/protocol/redact/*.pb.*

protos = $(wildcard $(PROTO_DIR)/*.proto)
proto: $(protos:.proto=.pb.go)

%.pb.go: %.proto
	@echo '>> protobuf bindings $*'
	$(PROTOC) --proto_path=$(PROTO_DIR) --go_out=. --go_opt=module=$(GO_MODULE) '$<'

# run go unit test with code coverage
test:
	@echo ">> running tests"
	@$(GO) test -short $(SRC) -cover

.PHONY: proto test
