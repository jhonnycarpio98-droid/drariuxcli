VERSION ?= v0.5.4
LDFLAGS := -s -w -X github.com/drariux/drariux-cli/cmd.Version=$(VERSION)

.PHONY: build dist release clean

build:
	go build -ldflags="$(LDFLAGS)" -o drariux .

# Cross-compile the four release targets into dist/.
dist:
	@mkdir -p dist
	@for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64; do \
		os=$${t%/*}; arch=$${t#*/}; \
		echo "building $$os-$$arch"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch \
			go build -ldflags="$(LDFLAGS)" -o dist/drariux .; \
		tar -czf dist/drariux_$(VERSION)_$$os_$$arch.tar.gz -C dist drariux; \
		rm -f dist/drariux; \
	done

# Build dist/ and publish a GitHub Release (requires: gh auth login).
release: dist
	gh release create $(VERSION) dist/*.tar.gz install.sh \
		--title "drariux $(VERSION)" --notes "Drariux CLI $(VERSION)" \
		|| gh release upload $(VERSION) dist/*.tar.gz --clobber

clean:
	rm -rf dist drariux
