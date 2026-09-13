# 11. Zero-Dependency npm Binary Distribution Wrapper

We decided to distribute TeleDrive on the npm registry as a zero-dependency binary distribution wrapper (`npx teledrive` / `npm install -g teledrive`).

Rather than rewriting the application in Node.js or distributing heavy platform-specific npm packages, TeleDrive ships a lightweight launcher script (`bin/teledrive.js`, < 10 KB) that inspects the host platform (`process.platform`) and architecture (`process.arch`), lazily downloads the appropriate pre-compiled native Go binary from official GitHub Releases into a local cache (`~/.cache/teledrive/bin/`), and transparently spawns the process passing all terminal arguments and stdio streams.

This eliminates the need for Node.js users to install the Go toolchain or manually extract archive files, preserves the pure-Go MTProto engine performance, and maintains zero third-party dependencies in `node_modules`.
