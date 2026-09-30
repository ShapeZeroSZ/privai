.PHONY: build install test clean

build:
	go build -ldflags="-s -w" -trimpath -o privai .

install: build
	sudo install -D privai /usr/local/bin/privai
	sudo install -D -m 644 privai.1 /usr/local/share/man/man1/privai.1

test:
	go test ./...

clean:
	rm -f privai
