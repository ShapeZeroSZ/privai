.PHONY: build install clean

build:
	go build -ldflags="-s -w" -trimpath -o privai main.go

install: build
	sudo install privai /usr/local/bin/
	sudo install privai.1 /usr/local/share/man/man1/

clean:
	rm -f privai
