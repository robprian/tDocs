# 2. Use Pure Go MTProto Client (gotd/td)

We decided to use `github.com/gotd/td` as our Telegram client library instead of `tdlib` (C++) or Rust FFI wrappers. `tdlib` requires complex C++ toolchains and cgo, which breaks cross-compilation and bloats the binary. `gotd/td` is 100% pure Go, supports streaming chunked uploads/downloads, and enables compiling TeleDrive as a portable, single static binary.
