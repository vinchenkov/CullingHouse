module mc

go 1.25.0

// The mise-pinned Go (1.24.5) resolves the go >= 1.25.0 requirement of
// modernc.org/sqlite via GOTOOLCHAIN; this directive makes the auto-selected
// toolchain an explicit, tracked pin instead of a floating download
// (NOTE(P1.17) in substrate/NOTES.md; §16.1 declared-toolchain rule).
toolchain go1.25.12

require modernc.org/sqlite v1.53.0

require (
	github.com/dustin/go-humanize v1.0.1 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/mattn/go-isatty v0.0.20 // indirect
	github.com/ncruces/go-strftime v1.0.0 // indirect
	github.com/remyoudompheng/bigfft v0.0.0-20230129092748-24d4a6f8daec // indirect
	golang.org/x/sys v0.44.0 // indirect
	modernc.org/libc v1.73.4 // indirect
	modernc.org/mathutil v1.7.1 // indirect
	modernc.org/memory v1.11.0 // indirect
)
