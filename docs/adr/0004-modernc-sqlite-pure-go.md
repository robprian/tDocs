# 4. Use Pure Go SQLite Driver (modernc.org/sqlite)

We decided to use `modernc.org/sqlite` instead of `mattn/go-sqlite3`. `mattn/go-sqlite3` requires cgo and an external C compiler toolchain, complicating cross-compilation and container builds. `modernc.org/sqlite` is 100% pure Go, allowing zero-cgo static binary builds for all target architectures while providing robust WAL-mode performance.
