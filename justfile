build:
    go build -o lz .

publish: build
    # Atomic replace via temp + mv: overwriting a signed Mach-O in place
    # invalidates its ad-hoc signature on Apple Silicon, causing the next
    # exec to be SIGKILLed ("killed: 9"). A fresh inode avoids that.
    cp lz ~/.local/bin/.lz.tmp
    mv -f ~/.local/bin/.lz.tmp ~/.local/bin/lz

vet:
    go vet ./...
