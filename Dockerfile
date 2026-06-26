# GoReleaser copies the prebuilt static binary in; no build stage needed.
# The promptr CLI is pure Go (CGO disabled) and does no network I/O, so scratch
# is enough — no libc, no CA certificates required.
FROM scratch
COPY promptr /promptr
ENTRYPOINT ["/promptr"]
