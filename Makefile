.PHONY: build vet sri clean

BINARY = aurora

build: sri vet
	go build -o $(BINARY) ./cmd/aurora

vet:
	go vet ./...

sri:
	@echo "Computing SRI hashes..."
	@HTMX_HASH=$$(openssl dgst -sha384 -binary internal/api/admin_static/htmx.min.js | openssl base64 -A) && \
	ADMIN_HASH=$$(openssl dgst -sha384 -binary internal/api/admin_static/admin.js | openssl base64 -A) && \
	echo "  htmx.min.js: sha384-$$HTMX_HASH" && \
	echo "  admin.js:    sha384-$$ADMIN_HASH"

clean:
	rm -f $(BINARY)
